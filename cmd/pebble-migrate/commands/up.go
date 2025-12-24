package commands

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	migrate "github.com/herenow/pebble-migrate"
)

// NewUpCommand creates the up command
func NewUpCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "up [target_version]",
		Short: "Apply pending migrations",
		Long: `Apply all pending migrations or migrate to a specific version.

Without arguments, this command applies all pending migrations to bring
the database to the latest version.

With a target version argument, it applies migrations up to and including
that specific version.

Examples:
  pebble-migrate up          # Apply all pending migrations
  pebble-migrate up 5        # Migrate to version 5
  pebble-migrate up --dry-run  # Show what would be done
  pebble-migrate up --no-backup  # Skip backup creation`,
		Args: cobra.MaximumNArgs(1),
		RunE: runUpCommand,
	}

	cmd.Flags().Bool("no-backup", false, "Skip creating backup before migration")

	return cmd
}

func runUpCommand(cmd *cobra.Command, args []string) error {
	config, err := GetGlobalConfig(cmd)
	if err != nil {
		return err
	}

	// Parse target version if provided (Unix timestamp)
	var targetVersion *int64
	if len(args) > 0 {
		version, err := strconv.ParseInt(args[0], 10, 64)
		if err != nil {
			return fmt.Errorf("invalid version number: %s", args[0])
		}
		targetVersion = &version
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

	// Validate schema state (only for non-dry-run)
	if !config.DryRun {
		if err := ValidateSchemaState(schemaManager); err != nil {
			return fmt.Errorf("database is not in a valid state for migration: %w", err)
		}
	}

	// Create migration plan
	var plan *migrate.ExecutionPlan
	if targetVersion != nil {
		plan, err = planner.PlanUpgradeTo(*targetVersion)
		if err != nil {
			return fmt.Errorf("failed to create migration plan: %w", err)
		}
	} else {
		plan, err = planner.PlanUpgrade()
		if err != nil {
			return fmt.Errorf("failed to create migration plan: %w", err)
		}
	}

	// Check if there are migrations to apply
	if len(plan.Migrations) == 0 {
		PrintSuccess("Database is already up to date!\n")
		return nil
	}

	// Display plan
	displayMigrationPlan(plan, config.DryRun)

	// Confirm execution (unless dry-run or non-interactive)
	if !config.DryRun {
		if !ConfirmAction("Do you want to proceed with this migration?") {
			PrintInfo("Migration cancelled.\n")
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

	// Execute migration plan with progress callback
	progressCallback := createProgressCallback(config.Verbose)
	err = engine.ExecutePlan(plan, progressCallback)
	if err != nil {
		PrintError("Migration failed: %v\n", err)
		return err
	}

	// Success message
	if config.DryRun {
		PrintSuccess("Dry run completed successfully. No changes were made.\n")
	} else {
		PrintSuccess("Migration completed successfully!\n")
		PrintInfo("Database is now at version %d\n", plan.TargetVersion)
	}

	return nil
}

func displayMigrationPlan(plan *migrate.ExecutionPlan, isDryRun bool) {
	prefix := ""
	if isDryRun {
		prefix = "[DRY RUN] "
	}

	fmt.Printf("=== %sMigration Plan ===\n", prefix)
	fmt.Printf("Current Version: %d\n", plan.CurrentVersion)
	fmt.Printf("Target Version: %d\n", plan.TargetVersion)
	fmt.Printf("Migrations to Apply: %d\n", len(plan.Migrations))
	fmt.Printf("\n")

	if len(plan.Migrations) > 0 {
		fmt.Printf("Migrations:\n")
		for i, m := range plan.Migrations {
			fmt.Printf("  %d. %s (v%d) - %s\n", i+1, m.ID, m.Version, m.Description)
		}
		fmt.Printf("\n")
	}
}

func createProgressCallback(verbose bool) func(string) {
	return func(message string) {
		if verbose {
			fmt.Printf("[PROGRESS] %s\n", message)
		} else {
			// For non-verbose mode, only show major progress indicators
			if len(message) > 0 && (strings.HasPrefix(message, "✓") || strings.HasPrefix(message, "⚠") || strings.HasPrefix(message, "✗")) {
				fmt.Println(message)
			}
		}
	}
}
