package commands

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/cockroachdb/pebble"
	"github.com/spf13/cobra"
	migrate "github.com/herenow/pebble-migrate"
)

// Common configuration and utilities for CLI commands

// GlobalConfig holds common configuration for all commands
type GlobalConfig struct {
	DatabasePath string
	Verbose      bool
	DryRun       bool
}

// GetGlobalConfig extracts global configuration from cobra command
func GetGlobalConfig(cmd *cobra.Command) (*GlobalConfig, error) {
	dbPath, err := cmd.Flags().GetString("database")
	if err != nil {
		return nil, fmt.Errorf("failed to get database flag: %w", err)
	}

	verbose, err := cmd.Flags().GetBool("verbose")
	if err != nil {
		return nil, fmt.Errorf("failed to get verbose flag: %w", err)
	}

	dryRun, err := cmd.Flags().GetBool("dry-run")
	if err != nil {
		return nil, fmt.Errorf("failed to get dry-run flag: %w", err)
	}

	// Validate database path
	if dbPath == "" {
		return nil, fmt.Errorf("database path is required")
	}

	// Convert to absolute path
	dbPath, err = filepath.Abs(dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to get absolute path for database: %w", err)
	}

	return &GlobalConfig{
		DatabasePath: dbPath,
		Verbose:      verbose,
		DryRun:       dryRun,
	}, nil
}

// OpenDatabase opens a Pebble database connection
func OpenDatabase(dbPath string, readOnly bool) (*pebble.DB, error) {
	// Check if database directory exists
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		if readOnly {
			return nil, fmt.Errorf("database directory does not exist: %s", dbPath)
		}

		// Create directory if it doesn't exist
		if err := os.MkdirAll(dbPath, 0755); err != nil {
			return nil, fmt.Errorf("failed to create database directory: %w", err)
		}
	}

	opts := &pebble.Options{
		ReadOnly: readOnly,
	}

	db, err := pebble.Open(dbPath, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to open database at %s: %w", dbPath, err)
	}

	return db, nil
}

// CreateMigrationServices creates the core migration services
func CreateMigrationServices(db *pebble.DB) (*migrate.SchemaManager, *migrate.MigrationPlanner, *migrate.DiscoveryService) {
	schemaManager := migrate.NewSchemaManager(db)
	registry := migrate.GlobalRegistry
	planner := migrate.NewMigrationPlanner(registry, schemaManager)
	discovery := migrate.NewDiscoveryService("migrations", registry)

	return schemaManager, planner, discovery
}

// CreateMigrationEngine creates a migration engine with backup support
func CreateMigrationEngine(db *pebble.DB, dbPath string) (*migrate.MigrationEngine, *migrate.SchemaManager) {
	schemaManager := migrate.NewSchemaManager(db)
	engine := migrate.NewMigrationEngineWithBackup(db, schemaManager, migrate.GlobalRegistry, dbPath)

	return engine, schemaManager
}

// VerbosePrintf prints a message only if verbose mode is enabled
func VerbosePrintf(config *GlobalConfig, format string, args ...interface{}) {
	if config.Verbose {
		fmt.Printf("[VERBOSE] "+format, args...)
	}
}

// PrintSuccess prints a success message with a checkmark
func PrintSuccess(format string, args ...interface{}) {
	fmt.Printf("✓ "+format, args...)
}

// PrintWarning prints a warning message
func PrintWarning(format string, args ...interface{}) {
	fmt.Printf("⚠ "+format, args...)
}

// PrintError prints an error message
func PrintError(format string, args ...interface{}) {
	fmt.Printf("✗ "+format, args...)
}

// PrintInfo prints an informational message
func PrintInfo(format string, args ...interface{}) {
	fmt.Printf("ℹ "+format, args...)
}

// ConfirmAction prompts the user for confirmation
func ConfirmAction(message string) bool {
	fmt.Printf("%s (y/N): ", message)

	var response string
	fmt.Scanln(&response)

	return response == "y" || response == "Y" || response == "yes" || response == "Yes"
}

// FormatDuration formats a duration string for display
func FormatDuration(duration string) string {
	if duration == "" {
		return "unknown"
	}
	return duration
}

// ValidateSchemaState checks if the database is in a valid state for operations
func ValidateSchemaState(schemaManager *migrate.SchemaManager) error {
	return schemaManager.ValidateSchemaState()
}
