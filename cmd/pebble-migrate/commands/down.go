package commands

import (
	"fmt"
	"strconv"

	"github.com/spf13/cobra"
	migrate "github.com/herenow/pebble-migrate"
)

// NewDownCommand creates the down command
func NewDownCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "down <target_version>",
		Short: "Rollback migrations to a specific version",
		Long: `Rollback migrations to a specific target version.

This command rolls back migrations from the current version down to
the specified target version. All migrations after the target version
will be rolled back in reverse order.

WARNING: This operation can be destructive and may result in data loss.
Always backup your data before performing rollbacks.

Examples:
  pebble-migrate down 3       # Rollback to version 3
  pebble-migrate down 0       # Rollback all migrations
  pebble-migrate down 3 --dry-run  # Show what would be done
  pebble-migrate down 3 --no-backup  # Skip backup creation`,
		Args: cobra.ExactArgs(1),
		RunE: runDownCommand,
	}

	cmd.Flags().Bool("no-backup", false, "Skip creating backup before rollback")

	return cmd
}

func runDownCommand(cmd *cobra.Command, args []string) error {
	config, err := GetGlobalConfig(cmd)
	if err != nil {
		return err
	}

	// Parse target version (Unix timestamp)
	targetVersion, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil {
		return fmt.Errorf("invalid version number: %s", args[0])
	}

	if targetVersion < 0 {
		return fmt.Errorf("target version cannot be negative: %d", targetVersion)
	}

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

	// Get current schema version
	currentSchema, err := schemaManager.GetSchemaVersion()
	if err != nil {
		return fmt.Errorf("failed to get current schema version: %w", err)
	}

	// Check if downgrade is necessary
	if targetVersion >= currentSchema.CurrentVersion {
		PrintInfo("Database is already at or below version %d (current: %d)\n", targetVersion, currentSchema.CurrentVersion)
		return nil
	}

	// Validate schema state (only for non-dry-run)
	if !config.DryRun {
		if err := ValidateSchemaState(schemaManager); err != nil {
			return fmt.Errorf("database is not in a valid state for rollback: %w", err)
		}
	}

	// Create downgrade plan
	plan, err := planner.PlanDowngrade(targetVersion)
	if err != nil {
		return fmt.Errorf("failed to create rollback plan: %w", err)
	}

	// Display rollback plan
	displayRollbackPlan(plan, config.DryRun)

	// Show warning about potential data loss
	if !config.DryRun {
		PrintWarning("DANGER: This operation will rollback migrations and may result in data loss!\n")
		PrintWarning("Make sure you have a backup of your data before proceeding.\n")
		fmt.Printf("\n")
	}

	// Confirm execution (unless dry-run)
	if !config.DryRun {
		if !ConfirmAction("Are you absolutely sure you want to proceed with this rollback?") {
			PrintInfo("Rollback cancelled.\n")
			return nil
		}

		// Double confirmation for potentially destructive operations
		if plan.CurrentVersion > 0 && targetVersion == 0 {
			fmt.Printf("\n")
			PrintWarning("You are about to rollback ALL migrations to version 0!\n")
			if !ConfirmAction("Type 'yes' to confirm you want to rollback everything") {
				PrintInfo("Rollback cancelled.\n")
				return nil
			}
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

	// Execute rollback plan with progress callback
	progressCallback := createProgressCallback(config.Verbose)
	err = engine.ExecutePlan(plan, progressCallback)
	if err != nil {
		PrintError("Rollback failed: %v\n", err)
		return err
	}

	// Success message
	if config.DryRun {
		PrintSuccess("Dry run completed successfully. No changes were made.\n")
	} else {
		PrintSuccess("Rollback completed successfully!\n")
		PrintInfo("Database is now at version %d\n", plan.TargetVersion)
	}

	return nil
}

func displayRollbackPlan(plan *migrate.ExecutionPlan, isDryRun bool) {
	prefix := ""
	if isDryRun {
		prefix = "[DRY RUN] "
	}

	fmt.Printf("=== %sRollback Plan ===\n", prefix)
	fmt.Printf("Current Version: %d\n", plan.CurrentVersion)
	fmt.Printf("Target Version: %d\n", plan.TargetVersion)
	fmt.Printf("Migrations to Rollback: %d\n", len(plan.Migrations))
	fmt.Printf("\n")

	if len(plan.Migrations) > 0 {
		fmt.Printf("Migrations (will be rolled back in this order):\n")
		for i, m := range plan.Migrations {
			fmt.Printf("  %d. %s (v%d) - %s\n", i+1, m.ID, m.Version, m.Description)
		}
		fmt.Printf("\n")
	}
}
