package commands

import (
	"fmt"

	"github.com/spf13/cobra"
	migrate "github.com/herenow/pebble-migrate"
)

// NewRepairCommand creates the repair command
func NewRepairCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "repair",
		Short: "Repair inconsistencies in migration state",
		Long: `Repair inconsistencies between AppliedMigrations and MigrationHistory.

This command fixes the error:
  "migration X marked as applied but no successful record in history"

This can happen when:
- A database was initialized with an older version of the migration system
- The schema state was manually modified
- A bug caused inconsistent state

The repair creates synthetic history records for any migrations that are
marked as applied but don't have corresponding history entries.

Examples:
  pebble-migrate repair -d /path/to/db
  pebble-migrate repair -d /path/to/db --dry-run`,
		RunE: runRepairCommand,
	}

	return cmd
}

func runRepairCommand(cmd *cobra.Command, args []string) error {
	config, err := GetGlobalConfig(cmd)
	if err != nil {
		return err
	}

	// Open database (not read-only unless dry-run)
	db, err := OpenDatabase(config.DatabasePath, config.DryRun)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer db.Close()

	schemaManager := migrate.NewSchemaManager(db)
	registry := migrate.GlobalRegistry

	fmt.Printf("=== Migration State Repair ===\n\n")

	// Show current state
	currentSchema, err := schemaManager.GetSchemaVersion()
	if err != nil {
		return fmt.Errorf("failed to get schema version: %w", err)
	}

	fmt.Printf("Current Version: %d (%s)\n", currentSchema.CurrentVersion, migrate.FormatVersionAsTime(currentSchema.CurrentVersion))
	fmt.Printf("Applied Migrations: %d\n", len(currentSchema.AppliedMigrations))
	fmt.Printf("History Records: %d\n", len(currentSchema.MigrationHistory))
	fmt.Printf("Status: %s\n\n", currentSchema.Status)

	// Check what needs repair
	successfulInHistory := make(map[string]bool)
	for _, record := range currentSchema.MigrationHistory {
		if record.Success && !isRollbackRecord(record.ID) {
			successfulInHistory[record.ID] = true
		}
	}

	var missingHistory []string
	for migrationID := range currentSchema.AppliedMigrations {
		if !successfulInHistory[migrationID] {
			missingHistory = append(missingHistory, migrationID)
		}
	}

	if len(missingHistory) == 0 {
		PrintSuccess("No repairs needed - migration state is consistent\n")
		return nil
	}

	PrintWarning("Found %d migrations missing history records:\n", len(missingHistory))
	for _, id := range missingHistory {
		fmt.Printf("  - %s\n", id)
	}
	fmt.Println()

	if config.DryRun {
		PrintInfo("Dry-run mode: no changes made\n")
		return nil
	}

	// Confirm repair
	if !ConfirmAction("Proceed with repair?") {
		fmt.Println("Repair cancelled")
		return nil
	}

	// Perform repair
	repaired, err := schemaManager.RepairMissingHistory(registry)
	if err != nil {
		return fmt.Errorf("repair failed: %w", err)
	}

	PrintSuccess("Repaired %d migration records:\n", len(repaired))
	for _, id := range repaired {
		fmt.Printf("  - %s\n", id)
	}

	// Validate after repair
	fmt.Println()
	PrintInfo("Validating repaired state...\n")
	if err := schemaManager.ValidateSchemaState(); err != nil {
		PrintError("Validation failed after repair: %v\n", err)
		return err
	}
	PrintSuccess("Validation passed!\n")

	return nil
}
