package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/herenow/pebble-migrate/cmd/pebble-migrate/commands"
)

// Version information (set during build)
var (
	Version   = "dev"
	BuildTime = "unknown"
	GitCommit = "unknown"
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "pebble-migrate",
		Short: "Database migration tool for Pebble",
		Long: `A comprehensive database migration tool for Pebble that provides
schema versioning, migration management, and data validation capabilities.

This tool allows you to:
- Upgrade your database schema to the latest version
- Rollback to previous schema versions
- Rerun specific migrations
- Validate data integrity
- View migration status and history`,
		Version: fmt.Sprintf("%s (built: %s, commit: %s)", Version, BuildTime, GitCommit),
	}

	// Add global flags
	rootCmd.PersistentFlags().StringP("database", "d", "", "Path to the Pebble database directory")
	rootCmd.PersistentFlags().BoolP("verbose", "v", false, "Enable verbose output")
	rootCmd.PersistentFlags().BoolP("dry-run", "n", false, "Show what would be done without executing")

	// Mark database flag as required
	rootCmd.MarkPersistentFlagRequired("database")

	// Add commands
	rootCmd.AddCommand(commands.NewStatusCommand())
	rootCmd.AddCommand(commands.NewUpCommand())
	rootCmd.AddCommand(commands.NewDownCommand())
	rootCmd.AddCommand(commands.NewRerunCommand())
	rootCmd.AddCommand(commands.NewValidateCommand())
	rootCmd.AddCommand(commands.NewCreateCommand())
	rootCmd.AddCommand(commands.NewHistoryCommand())
	rootCmd.AddCommand(commands.NewForceCleanCommand())
	rootCmd.AddCommand(commands.NewBackupCommand())

	// Execute the root command
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
