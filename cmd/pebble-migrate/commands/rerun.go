package commands

import (
	"fmt"
	"time"

	"github.com/cockroachdb/pebble"
	"github.com/spf13/cobra"
	migrate "github.com/herenow/pebble-migrate"
)

// NewRerunCommand creates the rerun command
func NewRerunCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rerun <migration_id>",
		Short: "Rerun a specific migration",
		Long: `Rerun a specific migration by rolling it back and then applying it again.

This command first rolls back the specified migration (runs its Down function)
and then reapplies it (runs its Up function). This is useful for:
- Testing migration rollback/forward compatibility
- Fixing issues with a specific migration
- Reprocessing data after migration logic changes

The schema version will remain the same after a successful rerun.

Examples:
  pebble-migrate rerun 001_add_indexes
  pebble-migrate rerun 002_update_schema --dry-run
  pebble-migrate rerun 001_test --no-backup`,
		Args: cobra.ExactArgs(1),
		RunE: runRerunCommand,
	}

	cmd.Flags().Bool("no-backup", false, "Skip creating backup before rerun")

	return cmd
}

func runRerunCommand(cmd *cobra.Command, args []string) error {
	config, err := GetGlobalConfig(cmd)
	if err != nil {
		return err
	}

	migrationID := args[0]

	// Open database (read-only for dry-run, read-write otherwise)
	readOnly := config.DryRun
	db, err := OpenDatabase(config.DatabasePath, readOnly)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer db.Close()

	// Create migration services
	schemaManager, planner, discovery := CreateMigrationServices(db)

	// Validate migrations
	if err := discovery.ValidateMigrations(); err != nil {
		return fmt.Errorf("migration validation failed: %w", err)
	}

	// Check if migration exists
	migrationRegistry := migrate.GlobalRegistry
	targetMigration, exists := migrationRegistry.GetMigration(migrationID)
	if !exists {
		return fmt.Errorf("migration '%s' not found", migrationID)
	}

	// Check if migration has been applied
	applied, err := schemaManager.IsMigrationApplied(migrationID)
	if err != nil {
		return fmt.Errorf("failed to check if migration is applied: %w", err)
	}

	if !applied {
		PrintWarning("Migration '%s' has not been applied yet.\n", migrationID)
		if !ConfirmAction("Do you want to apply it for the first time instead of rerunning?") {
			PrintInfo("Operation cancelled.\n")
			return nil
		}

		// Convert to a simple up migration
		return runFirstTimeApplication(targetMigration, config, db, schemaManager)
	}

	// Validate schema state (only for non-dry-run)
	if !config.DryRun {
		if err := ValidateSchemaState(schemaManager); err != nil {
			return fmt.Errorf("database is not in a valid state for rerun: %w", err)
		}
	}

	// Create rerun plan
	plan, err := planner.PlanRerun(migrationID)
	if err != nil {
		return fmt.Errorf("failed to create rerun plan: %w", err)
	}

	// Display rerun plan
	displayRerunPlan(plan, config.DryRun)

	// Show warning about potential risks
	if !config.DryRun {
		PrintWarning("CAUTION: Rerunning migrations can be risky and may cause data issues.\n")
		PrintWarning("Make sure you understand the migration's impact before proceeding.\n")
		fmt.Printf("\n")
	}

	// Confirm execution (unless dry-run)
	if !config.DryRun {
		if !ConfirmAction(fmt.Sprintf("Do you want to rerun migration '%s'?", migrationID)) {
			PrintInfo("Rerun cancelled.\n")
			return nil
		}
	}

	// Create migration engine with backup support
	engine, _ := CreateMigrationEngine(db, config.DatabasePath)
	engine.SetDryRun(config.DryRun)
	engine.SetVerbose(config.Verbose)

	// Check if backup should be disabled
	noBackup, _ := cmd.Flags().GetBool("no-backup")
	if noBackup {
		engine.SetBackupEnabled(false)
		if config.Verbose {
			PrintInfo("Backup creation disabled by --no-backup flag\n")
		}
	}

	// Execute rerun plan with progress callback
	progressCallback := createProgressCallback(config.Verbose)
	err = engine.ExecutePlan(plan, progressCallback)
	if err != nil {
		PrintError("Rerun failed: %v\n", err)
		return err
	}

	// Success message
	if config.DryRun {
		PrintSuccess("Dry run completed successfully. No changes were made.\n")
	} else {
		PrintSuccess("Migration rerun completed successfully!\n")
		PrintInfo("Migration '%s' has been rerun (version %d)\n", migrationID, targetMigration.Version)
	}

	return nil
}

func runFirstTimeApplication(targetMigration *migrate.Migration, config *GlobalConfig, db *pebble.DB, schemaManager *migrate.SchemaManager) error {
	// This is essentially a simplified up migration for a single migration
	PrintInfo("Applying migration '%s' for the first time...\n", targetMigration.ID)

	if config.DryRun {
		PrintInfo("DRY RUN: Would apply migration '%s' (v%d)\n", targetMigration.ID, targetMigration.Version)
		PrintInfo("Description: %s\n", targetMigration.Description)
		return nil
	}

	// Create a simple single-migration plan with backup support
	engine, _ := CreateMigrationEngine(db, config.DatabasePath)
	engine.SetVerbose(config.Verbose)

	// Mark migration as started
	if err := schemaManager.MarkMigrationStarted(); err != nil {
		return fmt.Errorf("failed to mark migration as started: %w", err)
	}

	// Execute the migration
	start := time.Now()
	if err := targetMigration.Up(db); err != nil {
		if markErr := schemaManager.MarkMigrationFailed(targetMigration.ID, targetMigration.Description, err); markErr != nil {
			return fmt.Errorf("migration failed and failed to mark as failed: %w (original error: %v)", markErr, err)
		}
		return fmt.Errorf("migration failed: %w", err)
	}

	// Run validation if available
	if targetMigration.Validate != nil {
		if err := targetMigration.Validate(db); err != nil {
			return fmt.Errorf("migration validation failed: %w", err)
		}
	}

	duration := time.Since(start)

	// Update schema after migration
	if err := schemaManager.UpdateSchemaAfterMigration(targetMigration.ID, targetMigration.Version, targetMigration.Description, duration); err != nil {
		return fmt.Errorf("failed to update schema after migration: %w", err)
	}

	PrintSuccess("Migration '%s' applied successfully!\n", targetMigration.ID)
	return nil
}

func displayRerunPlan(plan *migrate.ExecutionPlan, isDryRun bool) {
	prefix := ""
	if isDryRun {
		prefix = "[DRY RUN] "
	}

	fmt.Printf("=== %sRerun Plan ===\n", prefix)

	if len(plan.Migrations) > 0 {
		m := plan.Migrations[0]
		fmt.Printf("Migration: %s (v%d)\n", m.ID, m.Version)
		fmt.Printf("Description: %s\n", m.Description)
		fmt.Printf("Current Version: %d (will remain unchanged)\n", plan.CurrentVersion)
		fmt.Printf("\n")
		fmt.Printf("Steps:\n")
		fmt.Printf("  1. Rollback migration (run Down function)\n")
		fmt.Printf("  2. Reapply migration (run Up function)\n")
		fmt.Printf("  3. Run validation (if available)\n")
		fmt.Printf("\n")
	}
}
