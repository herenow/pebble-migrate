package commands

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
	migrate "github.com/herenow/pebble-migrate"
)

// NewBackupCommand creates the backup command
func NewBackupCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "backup",
		Short: "Database backup management",
		Long: `Manage database backups for safe migration operations.

This command provides subcommands for creating, listing, and managing
database backups that are automatically created before migrations.`,
	}

	cmd.AddCommand(NewBackupCreateCommand())
	cmd.AddCommand(NewBackupListCommand())
	cmd.AddCommand(NewBackupRestoreCommand())
	cmd.AddCommand(NewBackupCleanupCommand())

	return cmd
}

// NewBackupCreateCommand creates the backup create subcommand
func NewBackupCreateCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create [description]",
		Short: "Create a database backup",
		Long: `Create a manual backup of the database.

Examples:
  pebble-migrate backup create "Before major update"
  pebble-migrate backup create`,
		Args: cobra.MaximumNArgs(1),
		RunE: runBackupCreateCommand,
	}

	return cmd
}

// NewBackupListCommand creates the backup list subcommand
func NewBackupListCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List available backups",
		Long: `List all available backups for the database.

Shows backup creation time, size, version, and description.`,
		RunE: runBackupListCommand,
	}

	return cmd
}

// NewBackupRestoreCommand creates the backup restore subcommand
func NewBackupRestoreCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "restore <backup_path>",
		Short: "Restore database from backup",
		Long: `Restore the database from a specified backup.

WARNING: This will completely replace the current database with the backup.
Make sure to create a backup of the current state if needed.

Examples:
  pebble-migrate backup restore /path/to/db.backup_20240101_120000`,
		Args: cobra.ExactArgs(1),
		RunE: runBackupRestoreCommand,
	}

	cmd.Flags().Bool("force", false, "Skip confirmation prompt")

	return cmd
}

// NewBackupCleanupCommand creates the backup cleanup subcommand
func NewBackupCleanupCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cleanup",
		Short: "Clean up old backups",
		Long: `Remove old backups based on age.

Examples:
  pebble-migrate backup cleanup --older-than 30d
  pebble-migrate backup cleanup --older-than 7d`,
		RunE: runBackupCleanupCommand,
	}

	cmd.Flags().String("older-than", "30d", "Remove backups older than this duration (e.g., 7d, 30d, 24h)")

	return cmd
}

func runBackupCreateCommand(cmd *cobra.Command, args []string) error {
	config, err := GetGlobalConfig(cmd)
	if err != nil {
		return err
	}

	description := "Manual backup"
	if len(args) > 0 {
		description = args[0]
	}

	backupManager := migrate.NewBackupManager(config.DatabasePath)

	// Open database for backup
	db, err := OpenDatabase(config.DatabasePath, true)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer db.Close()

	PrintInfo("Creating backup of database: %s\n", config.DatabasePath)
	backupInfo, err := backupManager.CreateBackup(db, description)
	if err != nil {
		return fmt.Errorf("failed to create backup: %w", err)
	}

	PrintSuccess("✓ Backup created successfully!\n")
	fmt.Printf("  Path: %s\n", backupInfo.Path)
	fmt.Printf("  Size: %.2f MB\n", float64(backupInfo.Size)/1024/1024)
	fmt.Printf("  Version: %d\n", backupInfo.Version)
	fmt.Printf("  Description: %s\n", backupInfo.Description)

	return nil
}

func runBackupListCommand(cmd *cobra.Command, args []string) error {
	config, err := GetGlobalConfig(cmd)
	if err != nil {
		return err
	}

	backupManager := migrate.NewBackupManager(config.DatabasePath)

	backups, err := backupManager.ListBackups()
	if err != nil {
		return fmt.Errorf("failed to list backups: %w", err)
	}

	if len(backups) == 0 {
		PrintInfo("No backups found for database: %s\n", config.DatabasePath)
		return nil
	}

	fmt.Printf("=== Available Backups ===\n\n")
	fmt.Printf("Found %d backup(s) for database: %s\n\n", len(backups), config.DatabasePath)

	for i, backup := range backups {
		fmt.Printf("%d. %s\n", i+1, backup.Path)
		fmt.Printf("   Created: %s\n", backup.CreatedAt.Format("2006-01-02 15:04:05"))
		fmt.Printf("   Size: %.2f MB\n", float64(backup.Size)/1024/1024)
		fmt.Printf("   Version: %d\n", backup.Version)
		fmt.Printf("   Description: %s\n", backup.Description)
		fmt.Printf("\n")
	}

	return nil
}

func runBackupRestoreCommand(cmd *cobra.Command, args []string) error {
	config, err := GetGlobalConfig(cmd)
	if err != nil {
		return err
	}

	backupPath := args[0]
	force, _ := cmd.Flags().GetBool("force")

	backupManager := migrate.NewBackupManager(config.DatabasePath)

	// Confirm restore operation unless forced
	if !force {
		PrintWarning("WARNING: This will completely replace the current database!\n")
		PrintInfo("Current database: %s\n", config.DatabasePath)
		PrintInfo("Backup to restore: %s\n", backupPath)

		if !ConfirmAction("Do you want to proceed with the restore?") {
			PrintInfo("Restore cancelled.\n")
			return nil
		}
	}

	PrintInfo("Restoring database from backup...\n")
	err = backupManager.RestoreBackup(backupPath)
	if err != nil {
		return fmt.Errorf("failed to restore backup: %w", err)
	}

	PrintSuccess("✓ Database restored successfully from backup!\n")
	return nil
}

func runBackupCleanupCommand(cmd *cobra.Command, args []string) error {
	config, err := GetGlobalConfig(cmd)
	if err != nil {
		return err
	}

	olderThanStr, _ := cmd.Flags().GetString("older-than")
	olderThan, err := time.ParseDuration(olderThanStr)
	if err != nil {
		// Try parsing as days if not a valid duration
		if olderThanStr[len(olderThanStr)-1] == 'd' {
			days := olderThanStr[:len(olderThanStr)-1]
			var numDays int
			if _, err := fmt.Sscanf(days, "%d", &numDays); err == nil {
				olderThan = time.Duration(numDays) * 24 * time.Hour
			} else {
				return fmt.Errorf("invalid duration format: %s", olderThanStr)
			}
		} else {
			return fmt.Errorf("invalid duration format: %s (use format like '30d', '7d', '24h')", olderThanStr)
		}
	}

	backupManager := migrate.NewBackupManager(config.DatabasePath)

	PrintInfo("Cleaning up backups older than %v...\n", olderThan)
	err = backupManager.CleanupOldBackups(olderThan)
	if err != nil {
		return fmt.Errorf("failed to cleanup backups: %w", err)
	}

	return nil
}
