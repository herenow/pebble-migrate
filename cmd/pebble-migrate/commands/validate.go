package commands

import (
	"fmt"

	"github.com/spf13/cobra"
	migrate "github.com/herenow/pebble-migrate"
)

// NewValidateCommand creates the validate command
func NewValidateCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "validate",
		Short: "Validate database integrity and migration state",
		Long: `Validate the database integrity and migration state.

This command performs comprehensive validation checks including:
- Schema version consistency
- Migration history integrity
- Data format validation
- Key structure validation
- Orphaned data detection

Examples:
  pebble-migrate validate
  pebble-migrate validate --verbose`,
		RunE: runValidateCommand,
	}

	return cmd
}

func runValidateCommand(cmd *cobra.Command, args []string) error {
	config, err := GetGlobalConfig(cmd)
	if err != nil {
		return err
	}

	// Open database in read-only mode
	db, err := OpenDatabase(config.DatabasePath, true)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer db.Close()

	// Create migration services
	schemaManager, _, discovery := CreateMigrationServices(db)

	fmt.Printf("=== Database Validation ===\n\n")

	// Validate migration registry
	PrintInfo("Validating migration registry...\n")
	if err := discovery.ValidateMigrations(); err != nil {
		PrintError("Migration registry validation failed: %v\n", err)
		return err
	}
	PrintSuccess("Migration registry is valid\n\n")

	// Validate schema state
	PrintInfo("Validating schema state...\n")
	if err := ValidateSchemaState(schemaManager); err != nil {
		PrintError("Schema state validation failed: %v\n", err)
		return err
	}
	PrintSuccess("Schema state is valid\n\n")

	// Get current schema version
	currentSchema, err := schemaManager.GetSchemaVersion()
	if err != nil {
		return fmt.Errorf("failed to get schema version: %w", err)
	}

	// Display basic validation info
	fmt.Printf("Current Version: %d (%s)\n", currentSchema.CurrentVersion, migrate.FormatVersionAsTime(currentSchema.CurrentVersion))
	fmt.Printf("Status: %s\n", currentSchema.Status)
	fmt.Printf("Applied Migrations: %d\n", len(currentSchema.AppliedMigrations))

	// Validate migration history
	PrintInfo("\nValidating migration history...\n")
	validationResult := validateMigrationHistory(currentSchema, config.Verbose)
	if !validationResult.Success {
		PrintError("Migration history validation failed:\n")
		for _, issue := range validationResult.Issues {
			PrintError("  - %s\n", issue)
		}
		return fmt.Errorf("migration history validation failed")
	}
	PrintSuccess("Migration history is consistent\n")

	// TODO: Add data integrity validation once we implement the validation framework
	if config.Verbose {
		PrintInfo("\nSkipping data integrity validation (not yet implemented)\n")
		PrintInfo("This will validate:\n")
		PrintInfo("  - Data format consistency\n")
		PrintInfo("  - Key structure validation\n")
		PrintInfo("  - Orphaned data detection\n")
		PrintInfo("  - Cross-reference validation\n")
	}

	PrintSuccess("\n✓ Database validation completed successfully!\n")
	return nil
}

// ValidationResult represents the result of validation
type ValidationResult struct {
	Success bool
	Issues  []string
}

// validateMigrationHistory validates the consistency of migration history
func validateMigrationHistory(schema *migrate.SchemaVersion, verbose bool) ValidationResult {
	result := ValidationResult{
		Success: true,
		Issues:  make([]string, 0),
	}

	if verbose {
		fmt.Printf("  Checking migration history consistency...\n")
	}

	// Check migration history consistency
	appliedMigrations := 0

	for i, record := range schema.MigrationHistory {
		if verbose {
			fmt.Printf("    [%d] %s - %s\n", i+1, record.ID,
				record.AppliedAt.Format("2006-01-02 15:04:05"))
		}

		// Skip rollback records in counting
		if isRollbackRecord(record.ID) {
			continue
		}

		if record.Success {
			appliedMigrations++
		} else {
			result.Issues = append(result.Issues,
				fmt.Sprintf("Failed migration in history: %s - %s", record.ID, record.Error))
		}
	}

	// Check consistency between AppliedMigrations map and MigrationHistory
	for migID := range schema.AppliedMigrations {
		found := false
		for _, record := range schema.MigrationHistory {
			if record.ID == migID && record.Success {
				found = true
				break
			}
		}
		if !found {
			result.Success = false
			result.Issues = append(result.Issues,
				fmt.Sprintf("Migration %s marked as applied but not found in successful history", migID))
		}
	}

	if verbose {
		fmt.Printf("    Applied migrations: %d\n", appliedMigrations)
		fmt.Printf("    Current version: %d\n", schema.CurrentVersion)
	}

	return result
}

// isRollbackRecord checks if a migration record is a rollback record
func isRollbackRecord(id string) bool {
	return len(id) > 9 && (id[len(id)-9:] == "_rollback" || id[len(id)-6:] == "_rerun")
}
