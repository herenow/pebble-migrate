package commands

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
	migrate "github.com/herenow/pebble-migrate"
)

// NewStatusCommand creates the status command
func NewStatusCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show current migration status",
		Long: `Display the current schema version, applied migrations, and pending migrations.

This command provides a comprehensive overview of your database migration state,
including:
- Current schema version
- Migration status (clean, dirty, migrating)
- List of applied migrations with timestamps
- List of pending migrations
- Migration history and statistics`,
		RunE: runStatusCommand,
	}

	return cmd
}

func runStatusCommand(cmd *cobra.Command, args []string) error {
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
	schemaManager, planner, discovery := CreateMigrationServices(db)

	// Validate migrations
	if err := discovery.ValidateMigrations(); err != nil {
		PrintWarning("Migration validation issues: %v\n", err)
	}

	// Get current schema version
	currentSchema, err := schemaManager.GetSchemaVersion()
	if err != nil {
		return fmt.Errorf("failed to get schema version: %w", err)
	}

	// Get migration plan for upgrade
	plan, err := planner.PlanUpgrade()
	if err != nil {
		return fmt.Errorf("failed to create migration plan: %w", err)
	}

	// Display status information
	displaySchemaStatus(currentSchema)
	displayMigrationHistory(currentSchema)
	displayPendingMigrations(plan)
	displayMigrationStatistics(currentSchema, plan)

	return nil
}

func displaySchemaStatus(schema *migrate.SchemaVersion) {
	fmt.Printf("=== Schema Status ===\n")
	fmt.Printf("Current Version: %d (%s)\n", schema.CurrentVersion, migrate.FormatVersionAsTime(schema.CurrentVersion))

	// Status with color/emoji indicators
	statusIcon := getStatusIcon(schema.Status)
	fmt.Printf("Status: %s %s\n", statusIcon, schema.Status)

	if !schema.LastMigrationAt.IsZero() {
		fmt.Printf("Last Migration: %s\n", schema.LastMigrationAt.Format(time.RFC3339))
	} else {
		fmt.Printf("Last Migration: Never\n")
	}
	fmt.Printf("\n")
}

func displayMigrationHistory(schema *migrate.SchemaVersion) {
	fmt.Printf("=== Migration History ===\n")

	if len(schema.MigrationHistory) == 0 {
		fmt.Printf("No migrations have been applied.\n\n")
		return
	}

	// Display recent migrations (last 5)
	recentCount := 5
	start := len(schema.MigrationHistory) - recentCount
	if start < 0 {
		start = 0
	}

	fmt.Printf("Recent migrations (showing last %d):\n", min(len(schema.MigrationHistory), recentCount))
	for i := len(schema.MigrationHistory) - 1; i >= start; i-- {
		record := schema.MigrationHistory[i]
		statusIcon := "✓"
		if !record.Success {
			statusIcon = "✗"
		}

		fmt.Printf("  %s %s - %s\n",
			statusIcon, record.ID, record.AppliedAt.Format("2006-01-02 15:04:05"))

		if record.Duration != "" {
			fmt.Printf("    Duration: %s\n", record.Duration)
		}

		if record.Error != "" {
			fmt.Printf("    Error: %s\n", record.Error)
		}
	}

	if len(schema.MigrationHistory) > recentCount {
		fmt.Printf("  ... and %d more migrations\n", len(schema.MigrationHistory)-recentCount)
	}

	fmt.Printf("\n")
}

func displayPendingMigrations(plan *migrate.ExecutionPlan) {
	fmt.Printf("=== Pending Migrations ===\n")

	if len(plan.Migrations) == 0 {
		PrintSuccess("Database is up to date!\n\n")
		return
	}

	fmt.Printf("Found %d pending migration(s):\n", len(plan.Migrations))
	for _, m := range plan.Migrations {
		fmt.Printf("  • %s (v%d) - %s\n", m.ID, m.Version, m.Description)
	}

	fmt.Printf("\nTo apply pending migrations, run: pebble-migrate up\n\n")
}

func displayMigrationStatistics(schema *migrate.SchemaVersion, plan *migrate.ExecutionPlan) {
	fmt.Printf("=== Statistics ===\n")

	totalMigrations := len(schema.MigrationHistory)
	successfulMigrations := 0
	failedMigrations := 0

	for _, record := range schema.MigrationHistory {
		if record.Success {
			successfulMigrations++
		} else {
			failedMigrations++
		}
	}

	fmt.Printf("Applied Migrations: %d\n", totalMigrations)
	fmt.Printf("  • Successful: %d\n", successfulMigrations)

	if failedMigrations > 0 {
		fmt.Printf("  • Failed: %d\n", failedMigrations)
	}

	fmt.Printf("Pending Migrations: %d\n", len(plan.Migrations))

	if len(plan.Migrations) > 0 {
		fmt.Printf("Target Version: %d\n", plan.TargetVersion)
	}
}

func getStatusIcon(status migrate.Status) string {
	switch status {
	case migrate.StatusClean:
		return "✓"
	case migrate.StatusMigrating:
		return "🔄"
	case migrate.StatusDirty:
		return "⚠"
	case migrate.StatusRollback:
		return "↶"
	default:
		return "?"
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
