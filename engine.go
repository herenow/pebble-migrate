package migrate

import (
	"fmt"
	"time"

	"github.com/cockroachdb/pebble"
)

// MigrationEngine handles the execution of migrations
type MigrationEngine struct {
	db            *pebble.DB
	schemaManager *SchemaManager
	registry      *MigrationRegistry
	backupManager *BackupManager
	dryRun        bool
	verbose       bool
	enableBackup  bool
}


// NewMigrationEngineWithBackup creates a new migration engine with backup functionality
func NewMigrationEngineWithBackup(db *pebble.DB, schemaManager *SchemaManager, registry *MigrationRegistry, dbPath string) *MigrationEngine {
	return &MigrationEngine{
		db:            db,
		schemaManager: schemaManager,
		registry:      registry,
		backupManager: NewBackupManager(dbPath),
		dryRun:        false,
		verbose:       false,
		enableBackup:  true,
	}
}

// SetDryRun enables or disables dry-run mode
func (e *MigrationEngine) SetDryRun(enabled bool) {
	e.dryRun = enabled
}

// SetVerbose enables or disables verbose output
func (e *MigrationEngine) SetVerbose(enabled bool) {
	e.verbose = enabled
}

// SetBackupEnabled enables or disables automatic backup creation
func (e *MigrationEngine) SetBackupEnabled(enabled bool) {
	e.enableBackup = enabled
}

// SetBackupManager sets the backup manager for the engine
func (e *MigrationEngine) SetBackupManager(backupManager *BackupManager) {
	e.backupManager = backupManager
}

// ExecutePlan executes a migration plan
func (e *MigrationEngine) ExecutePlan(plan *ExecutionPlan, progressCallback func(string)) error {
	if progressCallback == nil {
		progressCallback = func(string) {} // No-op callback
	}

	switch plan.Type {
	case ExecutionTypeUpgrade:
		return e.executeUpgrade(plan, progressCallback)
	case ExecutionTypeDowngrade:
		return e.executeDowngrade(plan, progressCallback)
	case ExecutionTypeRerun:
		return e.executeRerun(plan, progressCallback)
	default:
		return fmt.Errorf("unsupported execution type: %s", plan.Type)
	}
}

// executeUpgrade executes an upgrade plan
func (e *MigrationEngine) executeUpgrade(plan *ExecutionPlan, progressCallback func(string)) error {
	progressCallback("Starting upgrade...")

	if e.dryRun {
		return e.simulateUpgrade(plan, progressCallback)
	}

	// Create backup before migration if enabled and there are migrations to apply
	if e.enableBackup && e.backupManager != nil && len(plan.Migrations) > 0 {
		progressCallback("Creating database backup before migration...")
		description := fmt.Sprintf("Before upgrade to version %d (%d migrations)", plan.TargetVersion, len(plan.Migrations))
		backupInfo, err := e.backupManager.CreateBackup(e.db, description)
		if err != nil {
			return fmt.Errorf("failed to create backup before migration: %w", err)
		}
		progressCallback(fmt.Sprintf("Backup created: %s", backupInfo.Path))
	}

	// Validate schema state before starting
	if err := e.schemaManager.ValidateSchemaState(); err != nil {
		return fmt.Errorf("schema validation failed: %w", err)
	}

	// Mark migration as started
	if err := e.schemaManager.MarkMigrationStarted(); err != nil {
		return fmt.Errorf("failed to mark migration as started: %w", err)
	}

	// Execute each migration
	for i, migration := range plan.Migrations {
		progressCallback(fmt.Sprintf("Executing migration %d/%d: %s", i+1, len(plan.Migrations), migration.ID))

		start := time.Now()
		if err := e.executeSingleMigration(migration, true); err != nil {
			// Mark migration as failed
			if markErr := e.schemaManager.MarkMigrationFailed(migration.ID, migration.Description, err); markErr != nil {
				return fmt.Errorf("migration failed and failed to mark as failed: %w (original error: %v)", markErr, err)
			}
			return fmt.Errorf("migration %s failed: %w", migration.ID, err)
		}
		duration := time.Since(start)

		// Update schema version after successful migration
		if err := e.schemaManager.UpdateSchemaAfterMigration(migration.ID, migration.Version, migration.Description, duration); err != nil {
			return fmt.Errorf("failed to update schema version after migration %s: %w", migration.ID, err)
		}

		if e.verbose {
			progressCallback(fmt.Sprintf("Migration %s completed in %v", migration.ID, duration))
		}
	}

	progressCallback("Upgrade completed successfully")
	return nil
}

// executeDowngrade executes a downgrade plan
func (e *MigrationEngine) executeDowngrade(plan *ExecutionPlan, progressCallback func(string)) error {
	progressCallback("Starting downgrade...")

	if e.dryRun {
		return e.simulateDowngrade(plan, progressCallback)
	}

	// Create backup before rollback if enabled and there are migrations to rollback
	if e.enableBackup && e.backupManager != nil && len(plan.Migrations) > 0 {
		progressCallback("Creating database backup before rollback...")
		description := fmt.Sprintf("Before rollback to version %d (%d rollbacks)", plan.TargetVersion, len(plan.Migrations))
		backupInfo, err := e.backupManager.CreateBackup(e.db, description)
		if err != nil {
			return fmt.Errorf("failed to create backup before rollback: %w", err)
		}
		progressCallback(fmt.Sprintf("Backup created: %s", backupInfo.Path))
	}

	// Validate schema state before starting
	if err := e.schemaManager.ValidateSchemaState(); err != nil {
		return fmt.Errorf("schema validation failed: %w", err)
	}

	// Mark rollback as started
	if err := e.schemaManager.MarkRollbackStarted(); err != nil {
		return fmt.Errorf("failed to mark rollback as started: %w", err)
	}

	// Execute each migration rollback
	for i, migration := range plan.Migrations {
		progressCallback(fmt.Sprintf("Rolling back migration %d/%d: %s", i+1, len(plan.Migrations), migration.ID))

		start := time.Now()
		if err := e.executeSingleMigration(migration, false); err != nil {
			// Mark migration as failed
			if markErr := e.schemaManager.MarkMigrationFailed(migration.ID+"_rollback", "Rollback: "+migration.Description, err); markErr != nil {
				return fmt.Errorf("rollback failed and failed to mark as failed: %w (original error: %v)", markErr, err)
			}
			return fmt.Errorf("rollback of migration %s failed: %w", migration.ID, err)
		}
		duration := time.Since(start)

		// Update schema after successful rollback
		if err := e.schemaManager.UpdateAfterRollback(migration.ID, migration.Version, migration.Description); err != nil {
			return fmt.Errorf("failed to update schema after rollback of %s: %w", migration.ID, err)
		}

		if e.verbose {
			progressCallback(fmt.Sprintf("Rollback of %s completed in %v", migration.ID, duration))
		}
	}

	progressCallback("Downgrade completed successfully")
	return nil
}

// executeRerun executes a rerun plan (down then up)
func (e *MigrationEngine) executeRerun(plan *ExecutionPlan, progressCallback func(string)) error {
	if len(plan.Migrations) != 1 {
		return fmt.Errorf("rerun plan must contain exactly one migration, got %d", len(plan.Migrations))
	}

	migration := plan.Migrations[0]
	progressCallback(fmt.Sprintf("Rerunning migration: %s", migration.ID))

	if e.dryRun {
		return e.simulateRerun(plan, progressCallback)
	}

	// Create backup before rerun if enabled
	if e.enableBackup && e.backupManager != nil {
		progressCallback("Creating database backup before rerun...")
		description := fmt.Sprintf("Before rerun of migration %s", migration.ID)
		backupInfo, err := e.backupManager.CreateBackup(e.db, description)
		if err != nil {
			return fmt.Errorf("failed to create backup before rerun: %w", err)
		}
		progressCallback(fmt.Sprintf("Backup created: %s", backupInfo.Path))
	}

	// Validate schema state before starting
	if err := e.schemaManager.ValidateSchemaState(); err != nil {
		return fmt.Errorf("schema validation failed: %w", err)
	}

	// Mark migration as started
	if err := e.schemaManager.MarkMigrationStarted(); err != nil {
		return fmt.Errorf("failed to mark migration as started: %w", err)
	}

	// Execute down migration first
	progressCallback(fmt.Sprintf("Rolling back migration: %s", migration.ID))
	if err := e.executeSingleMigration(migration, false); err != nil {
		if markErr := e.schemaManager.MarkMigrationFailed(migration.ID+"_rerun_rollback", "Rerun Rollback: "+migration.Description, err); markErr != nil {
			return fmt.Errorf("rerun rollback failed and failed to mark as failed: %w (original error: %v)", markErr, err)
		}
		return fmt.Errorf("rerun rollback of migration %s failed: %w", migration.ID, err)
	}

	// Execute up migration
	progressCallback(fmt.Sprintf("Re-applying migration: %s", migration.ID))
	start := time.Now()
	if err := e.executeSingleMigration(migration, true); err != nil {
		if markErr := e.schemaManager.MarkMigrationFailed(migration.ID+"_rerun", "Rerun: "+migration.Description, err); markErr != nil {
			return fmt.Errorf("rerun failed and failed to mark as failed: %w (original error: %v)", markErr, err)
		}
		return fmt.Errorf("rerun of migration %s failed: %w", migration.ID, err)
	}
	duration := time.Since(start)

	// Update schema version (should remain the same for rerun)
	if err := e.schemaManager.UpdateSchemaAfterMigration(migration.ID+"_rerun", migration.Version, "Rerun: "+migration.Description, duration); err != nil {
		return fmt.Errorf("failed to update schema version after rerun of %s: %w", migration.ID, err)
	}

	progressCallback(fmt.Sprintf("Rerun of migration %s completed successfully", migration.ID))
	return nil
}

// executeSingleMigration executes a single migration (up or down)
func (e *MigrationEngine) executeSingleMigration(migration *Migration, up bool) error {
	var migrationFunc MigrationFunc
	var direction string

	if up {
		migrationFunc = migration.Up
		direction = "up"
	} else {
		migrationFunc = migration.Down
		direction = "down"
	}

	if migrationFunc == nil {
		return fmt.Errorf("migration %s has no %s function", migration.ID, direction)
	}

	if e.verbose {
		fmt.Printf("Executing %s migration for %s...\n", direction, migration.ID)
	}

	// Execute the migration function
	if err := migrationFunc(e.db); err != nil {
		return fmt.Errorf("%s migration failed: %w", direction, err)
	}

	// Run validation if available
	if migration.Validate != nil {
		if e.verbose {
			fmt.Printf("Validating migration %s...\n", migration.ID)
		}

		if err := migration.Validate(e.db); err != nil {
			return fmt.Errorf("migration validation failed: %w", err)
		}
	}

	return nil
}

// Simulation methods for dry-run mode

func (e *MigrationEngine) simulateUpgrade(plan *ExecutionPlan, progressCallback func(string)) error {
	progressCallback("DRY RUN: Simulating upgrade...")

	for i, migration := range plan.Migrations {
		progressCallback(fmt.Sprintf("DRY RUN: Would execute migration %d/%d: %s", i+1, len(plan.Migrations), migration.ID))
		progressCallback(fmt.Sprintf("  Description: %s", migration.Description))
		progressCallback(fmt.Sprintf("  Version: %d (%s)", migration.Version, FormatVersionAsTime(migration.Version)))
	}

	progressCallback(fmt.Sprintf("DRY RUN: Would upgrade from version %d to %d", plan.CurrentVersion, plan.TargetVersion))
	return nil
}

func (e *MigrationEngine) simulateDowngrade(plan *ExecutionPlan, progressCallback func(string)) error {
	progressCallback("DRY RUN: Simulating downgrade...")

	for i, migration := range plan.Migrations {
		progressCallback(fmt.Sprintf("DRY RUN: Would rollback migration %d/%d: %s", i+1, len(plan.Migrations), migration.ID))
		progressCallback(fmt.Sprintf("  Description: %s", migration.Description))
		progressCallback(fmt.Sprintf("  Version: %d (%s)", migration.Version, FormatVersionAsTime(migration.Version)))
	}

	progressCallback(fmt.Sprintf("DRY RUN: Would downgrade from version %d to %d", plan.CurrentVersion, plan.TargetVersion))
	return nil
}

func (e *MigrationEngine) simulateRerun(plan *ExecutionPlan, progressCallback func(string)) error {
	if len(plan.Migrations) != 1 {
		return fmt.Errorf("rerun plan must contain exactly one migration, got %d", len(plan.Migrations))
	}

	migration := plan.Migrations[0]
	progressCallback("DRY RUN: Simulating rerun...")
	progressCallback(fmt.Sprintf("DRY RUN: Would rollback migration: %s", migration.ID))
	progressCallback(fmt.Sprintf("DRY RUN: Would re-apply migration: %s", migration.ID))
	progressCallback(fmt.Sprintf("  Description: %s", migration.Description))
	progressCallback(fmt.Sprintf("  Version: %d (unchanged) - %s", migration.Version, FormatVersionAsTime(migration.Version)))

	return nil
}
