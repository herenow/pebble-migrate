package migrate

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"github.com/cockroachdb/pebble"
)

// StartupOptions configures the migration startup behavior
type StartupOptions struct {
	// RunMigrations enables migration execution during startup if true
	// If false, will fail if migrations are needed
	RunMigrations bool

	// Logger is an optional logger for migration progress
	// If nil, uses fmt.Printf for logging
	Logger Logger

	// BackupEnabled controls whether backups are created during migration
	// Default: false (creating checkpoints and zipping is CPU intensive)
	BackupEnabled bool

	// CheckDiskSpace enables disk space validation before migrations
	// Default: true
	CheckDiskSpace bool

	// DatabaseSizeMultiplier is the space multiplier for migration space calculation
	// Required free space = database size * multiplier
	// Default: 2.0 (2x database size for backup + temporary doubling)
	DatabaseSizeMultiplier float64

	// CLIName is the name of the CLI tool shown in error messages
	// Default: "pebble-migrate"
	CLIName string
}

// DefaultStartupOptions returns default startup options
func DefaultStartupOptions() StartupOptions {
	return StartupOptions{
		RunMigrations:          false, // Migrations must be explicitly enabled
		Logger:                 nil,
		BackupEnabled:          false, // Disabled by default - creating checkpoints and zipping is CPU intensive
		CheckDiskSpace:         true,  // Enable disk space checking by default
		DatabaseSizeMultiplier: 2.0,   // Require 2x database size in free space
		CLIName:                "pebble-migrate",
	}
}

// CheckAndRunStartupMigrations checks migration status and optionally runs migrations
// This is a utility function for application startup integration
func CheckAndRunStartupMigrations(db *pebble.DB, dbPath string, opts StartupOptions) error {
	// Create migration services
	schemaManager := NewSchemaManager(db)
	registry := GlobalRegistry

	// Initialize schema for fresh/pre-migration databases
	if err := schemaManager.InitializeFreshDatabase(registry); err != nil {
		return fmt.Errorf("failed to initialize database schema: %w", err)
	}

	planner := NewMigrationPlanner(registry, schemaManager)

	// Check current schema version
	currentSchema, err := schemaManager.GetSchemaVersion()
	if err != nil {
		return fmt.Errorf("failed to get schema version: %w", err)
	}

	cliName := opts.CLIName
	if cliName == "" {
		cliName = "pebble-migrate"
	}

	// Check database state and attempt recovery if possible
	if currentSchema.Status == StatusMigrating {
		// Attempt to recover from interrupted migration
		if err := attemptMigrationRecovery(db, schemaManager, planner, opts); err != nil {
			return err
		}

		// Re-fetch schema after potential recovery
		currentSchema, err = schemaManager.GetSchemaVersion()
		if err != nil {
			return fmt.Errorf("failed to get schema version after recovery attempt: %w", err)
		}
	}

	// If still not clean after recovery attempt, fail
	if currentSchema.Status != StatusClean {
		return fmt.Errorf("database is in '%s' state - manual intervention required. "+
			"Run '%s status' to check and resolve issues", currentSchema.Status, cliName)
	}

	// Check for pending migrations
	plan, err := planner.PlanUpgrade()
	if err != nil {
		return fmt.Errorf("failed to create migration plan: %w", err)
	}

	if len(plan.Migrations) == 0 {
		if opts.Logger != nil {
			opts.Logger.Debugf("Database is up to date (version %d)", currentSchema.CurrentVersion)
		}
		return nil
	}

	// Handle pending migrations
	if !opts.RunMigrations {
		return fmt.Errorf("database has %d pending migrations. "+
			"Run migrations using '%s up' or restart with --migrate flag. "+
			"Note: After using --clean or with a new database, you must also pass --migrate",
			len(plan.Migrations), cliName)
	}

	// Check disk space before proceeding with migrations
	if opts.CheckDiskSpace {
		if err := checkMigrationDiskSpace(dbPath, opts.DatabaseSizeMultiplier, opts.Logger); err != nil {
			return fmt.Errorf("disk space check failed: %w", err)
		}
	}

	// Log migration start
	if opts.Logger != nil {
		opts.Logger.Printf("Running startup migrations (current: %d, target: %d, count: %d)",
			plan.CurrentVersion, plan.TargetVersion, len(plan.Migrations))
	}

	// Create migration engine with backup enabled
	engine := NewMigrationEngineWithBackup(db, schemaManager, registry, dbPath)
	engine.SetVerbose(false) // Let logger handle verbosity through log levels
	engine.SetBackupEnabled(opts.BackupEnabled)

	// Create progress callback that uses the logger
	progressCallback := func(msg string) {
		if opts.Logger != nil {
			opts.Logger.Debugf("%s", msg)
		} else {
			fmt.Printf("[MIGRATION] %s\n", msg)
		}
	}

	// Execute migrations with progress logging
	err = engine.ExecutePlan(plan, progressCallback)
	if err != nil {
		return fmt.Errorf("startup migration failed: %w", err)
	}

	// Log completion
	if opts.Logger != nil {
		opts.Logger.Printf("Startup migrations completed successfully (version %d)", plan.TargetVersion)
	}
	return nil
}


// attemptMigrationRecovery tries to recover from an interrupted migration
func attemptMigrationRecovery(db *pebble.DB, schemaManager *SchemaManager, planner *MigrationPlanner, opts StartupOptions) error {
	// Get current schema state
	currentSchema, err := schemaManager.GetSchemaVersion()
	if err != nil {
		return fmt.Errorf("failed to get schema version: %w", err)
	}

	cliName := opts.CLIName
	if cliName == "" {
		cliName = "pebble-migrate"
	}

	// Get pending migrations to identify what was likely being executed
	plan, err := planner.PlanUpgrade()
	if err != nil {
		return fmt.Errorf("failed to create migration plan for recovery: %w", err)
	}

	if len(plan.Migrations) == 0 {
		// No pending migrations but status is migrating - inconsistent state
		return fmt.Errorf("database is in 'migrating' state but no pending migrations found. "+
			"Run '%s force-clean' to manually reset state", cliName)
	}

	// The first pending migration is likely the one that was interrupted
	stuckMigration := plan.Migrations[0]

	// Check if the migration is safe to rerun
	if !stuckMigration.Rerunnable {
		return fmt.Errorf("database is in 'migrating' state - migration '%s' (%s) was interrupted. "+
			"This migration is not marked as rerunnable and requires manual intervention. "+
			"Options:\n"+
			"  1. Run '%s validate' to check if migration completed successfully\n"+
			"  2. Run '%s force-clean' to force reset (use with caution)\n"+
			"  3. Restore from backup if available",
			stuckMigration.ID, stuckMigration.Description, cliName, cliName)
	}

	// Migration is rerunnable - attempt recovery
	if opts.Logger != nil {
		opts.Logger.Printf("Recovering from interrupted migration: %s (%s)",
			stuckMigration.ID, stuckMigration.Description)
	} else {
		fmt.Printf("Recovering from interrupted migration: %s (%s)\n",
			stuckMigration.ID, stuckMigration.Description)
	}

	// Reset status to clean to allow retry
	currentSchema.Status = StatusClean
	if err := schemaManager.SetSchemaVersion(currentSchema); err != nil {
		return fmt.Errorf("failed to reset schema status for recovery: %w", err)
	}

	if opts.Logger != nil {
		opts.Logger.Printf("Migration state reset to clean, will retry migration")
	} else {
		fmt.Printf("Migration state reset to clean, will retry migration\n")
	}

	return nil
}

// checkMigrationDiskSpace validates available disk space using smart calculation
func checkMigrationDiskSpace(dbPath string, sizeMultiplier float64, logger Logger) error {
	// Calculate database size
	dbSize, err := calculateDatabaseSize(dbPath)
	if err != nil {
		if logger != nil {
			logger.Debugf("Could not calculate database size, skipping space check: %v", err)
		}
		return nil // Skip check if we can't calculate size
	}

	// Calculate required space
	requiredSpace := uint64(float64(dbSize) * sizeMultiplier)

	// Get filesystem statistics
	var stat syscall.Statfs_t
	if err := syscall.Statfs(dbPath, &stat); err != nil {
		if logger != nil {
			logger.Debugf("Disk space check not available on this system: %v", err)
		}
		return nil
	}

	// Calculate space statistics
	freeSpace := stat.Bavail * uint64(stat.Bsize)

	if logger != nil {
		logger.Debugf("Migration disk space check: db=%.2fGB, required=%.2fGB, free=%.2fGB, multiplier=%.1f",
			float64(dbSize)/(1024*1024*1024),
			float64(requiredSpace)/(1024*1024*1024),
			float64(freeSpace)/(1024*1024*1024),
			sizeMultiplier)
	}

	// Check if we have enough free space
	if freeSpace < requiredSpace {
		return fmt.Errorf("insufficient disk space for migration: %.2f GB required (%.2f GB database x %.1fx), only %.2f GB available",
			float64(requiredSpace)/(1024*1024*1024),
			float64(dbSize)/(1024*1024*1024),
			sizeMultiplier,
			float64(freeSpace)/(1024*1024*1024))
	}

	if logger != nil {
		logger.Printf("Migration disk space check passed: %.2fGB required, %.2fGB available",
			float64(requiredSpace)/(1024*1024*1024),
			float64(freeSpace)/(1024*1024*1024))
	}

	return nil
}

// calculateDatabaseSize calculates the total size of the database directory
func calculateDatabaseSize(dbPath string) (uint64, error) {
	var totalSize uint64

	err := filepath.Walk(dbPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			totalSize += uint64(info.Size())
		}
		return nil
	})

	return totalSize, err
}
