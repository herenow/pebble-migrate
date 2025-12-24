package migrate

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cockroachdb/pebble"
)

// BackupManager handles database backup and restore operations
type BackupManager struct {
	dbPath            string
	compress          bool
	cleanupOldBackups bool
	maxBackups        int
}

// NewBackupManager creates a new backup manager with default settings
func NewBackupManager(dbPath string) *BackupManager {
	return &BackupManager{
		dbPath:            dbPath,
		compress:          true, // Enable compression by default
		cleanupOldBackups: true, // Enable cleanup by default for operational sanity
		maxBackups:        2,    // Keep max 2 backups when cleanup is enabled
	}
}

// BackupOptions configures backup behavior
type BackupOptions struct {
	Compress          bool
	CleanupOldBackups bool
	MaxBackups        int
}


// BackupInfo contains information about a database backup
type BackupInfo struct {
	Path        string    `json:"path"`
	OriginalDB  string    `json:"original_db"`
	CreatedAt   time.Time `json:"created_at"`
	Size        int64     `json:"size"`
	Version     int32     `json:"version"`
	Description string    `json:"description"`
}

// CreateBackup creates a backup of the database before migration using Pebble Checkpoint
func (b *BackupManager) CreateBackup(db *pebble.DB, description string) (*BackupInfo, error) {
	timestamp := time.Now().Format("20060102_150405")

	var backupPath string
	var size int64
	var err error

	if b.compress {
		// Create compressed tar.gz backup using checkpoint
		backupPath = fmt.Sprintf("%s.backup_%s.tar.gz", b.dbPath, timestamp)
		fmt.Printf("Creating compressed backup: %s\n", backupPath)
		size, err = b.createCompressedCheckpointBackup(db, backupPath)
	} else {
		// Create uncompressed directory backup using checkpoint
		backupPath = fmt.Sprintf("%s.backup_%s", b.dbPath, timestamp)
		fmt.Printf("Creating backup: %s\n", backupPath)
		size, err = b.createCheckpointBackup(db, backupPath)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to create backup: %w", err)
	}

	// Get current schema version from open database
	version := int32(0)
	schemaManager := NewSchemaManager(db)
	if schema, err := schemaManager.GetSchemaVersion(); err == nil {
		// Convert int64 timestamp to int32 for backward compatibility
		// This is safe as we're within the valid int32 range for timestamps
		if schema.CurrentVersion <= int64(^int32(0)) {
			version = int32(schema.CurrentVersion)
		}
	}

	backupInfo := &BackupInfo{
		Path:        backupPath,
		OriginalDB:  b.dbPath,
		CreatedAt:   time.Now(),
		Size:        size,
		Version:     version,
		Description: description,
	}

	// Cleanup old backups if enabled
	if b.cleanupOldBackups {
		if err := b.performBackupCleanup(); err != nil {
			fmt.Printf("Warning: failed to cleanup old backups: %v\n", err)
		}
	}

	// Write backup metadata
	if err := b.writeBackupMetadata(backupInfo); err != nil {
		return nil, fmt.Errorf("failed to write backup metadata: %w", err)
	}

	fmt.Printf("Backup created successfully: %s (%.2f MB)\n",
		backupPath, float64(size)/1024/1024)

	return backupInfo, nil
}

// RestoreBackup restores a database from backup
func (b *BackupManager) RestoreBackup(backupPath string) error {
	fmt.Printf("Restoring database from backup: %s\n", backupPath)

	// Verify backup exists and is valid
	if !b.isValidBackup(backupPath) {
		return fmt.Errorf("invalid backup directory: %s", backupPath)
	}

	// Read backup metadata
	backupInfo, err := b.readBackupMetadata(backupPath)
	if err != nil {
		return fmt.Errorf("failed to read backup metadata: %w", err)
	}

	// Verify this backup is for the current database
	if backupInfo.OriginalDB != b.dbPath {
		return fmt.Errorf("backup is for database %s, not %s",
			backupInfo.OriginalDB, b.dbPath)
	}

	// Create temporary backup of current state
	tempBackup := b.dbPath + ".restore_temp_" + time.Now().Format("20060102_150405")
	if err := b.createTempBackup(tempBackup); err != nil {
		return fmt.Errorf("failed to create temporary backup: %w", err)
	}
	defer func() {
		// Clean up temp backup on success, keep on failure
		if err == nil {
			os.RemoveAll(tempBackup)
		} else {
			fmt.Printf("Temporary backup kept at: %s\n", tempBackup)
		}
	}()

	// Remove current database
	if err := os.RemoveAll(b.dbPath); err != nil {
		return fmt.Errorf("failed to remove current database: %w", err)
	}

	// Restore from backup
	_, err = b.copyDatabaseFiles(backupPath, b.dbPath)
	if err != nil {
		// Try to restore from temp backup
		if restoreErr := b.restoreFromTemp(tempBackup); restoreErr != nil {
			return fmt.Errorf("restore failed and recovery failed: %w (original: %v)",
				restoreErr, err)
		}
		return fmt.Errorf("restore failed but database recovered: %w", err)
	}

	fmt.Printf("Database restored successfully from backup\n")
	fmt.Printf("  Backup created: %s\n", backupInfo.CreatedAt.Format("2006-01-02 15:04:05"))
	fmt.Printf("  Backup version: %d\n", backupInfo.Version)
	fmt.Printf("  Description: %s\n", backupInfo.Description)

	return nil
}

// ListBackups lists all available backups for this database
func (b *BackupManager) ListBackups() ([]*BackupInfo, error) {
	dbDir := filepath.Dir(b.dbPath)
	dbName := filepath.Base(b.dbPath)

	// Find all backup directories
	pattern := filepath.Join(dbDir, dbName+".backup_*")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("failed to find backups: %w", err)
	}

	var backups []*BackupInfo
	for _, backupPath := range matches {
		if b.isValidBackup(backupPath) {
			if info, err := b.readBackupMetadata(backupPath); err == nil {
				backups = append(backups, info)
			}
		}
	}

	return backups, nil
}

// CleanupOldBackups removes backups older than the specified duration
func (b *BackupManager) CleanupOldBackups(olderThan time.Duration) error {
	backups, err := b.ListBackups()
	if err != nil {
		return fmt.Errorf("failed to list backups: %w", err)
	}

	cutoff := time.Now().Add(-olderThan)
	removedCount := 0

	for _, backup := range backups {
		if backup.CreatedAt.Before(cutoff) {
			fmt.Printf("Removing old backup: %s\n", backup.Path)
			if err := os.RemoveAll(backup.Path); err != nil {
				fmt.Printf("Warning: failed to remove backup %s: %v\n", backup.Path, err)
			} else {
				removedCount++
			}
		}
	}

	if removedCount > 0 {
		fmt.Printf("Removed %d old backup(s)\n", removedCount)
	} else {
		fmt.Printf("No old backups to remove\n")
	}

	return nil
}

// copyDatabaseFiles copies all database files from source to destination
func (b *BackupManager) copyDatabaseFiles(srcPath, dstPath string) (int64, error) {
	var totalSize int64

	return totalSize, filepath.Walk(srcPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip directories
		if info.IsDir() {
			return nil
		}

		// Calculate relative path
		relPath, err := filepath.Rel(srcPath, path)
		if err != nil {
			return err
		}

		dstFile := filepath.Join(dstPath, relPath)

		// Create destination directory if needed
		if err := os.MkdirAll(filepath.Dir(dstFile), 0755); err != nil {
			return err
		}

		// Copy file
		size, err := b.copyFile(path, dstFile)
		if err != nil {
			return err
		}

		totalSize += size
		return nil
	})
}

// copyFile copies a single file from source to destination
func (b *BackupManager) copyFile(src, dst string) (int64, error) {
	srcFile, err := os.Open(src)
	if err != nil {
		return 0, err
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dst)
	if err != nil {
		return 0, err
	}
	defer dstFile.Close()

	size, err := io.Copy(dstFile, srcFile)
	if err != nil {
		return 0, err
	}

	// Copy permissions
	if info, err := os.Stat(src); err == nil {
		os.Chmod(dst, info.Mode())
	}

	return size, nil
}

// createTempBackup creates a temporary backup for restore safety
func (b *BackupManager) createTempBackup(tempPath string) error {
	_, err := b.copyDatabaseFiles(b.dbPath, tempPath)
	return err
}

// restoreFromTemp restores from temporary backup
func (b *BackupManager) restoreFromTemp(tempPath string) error {
	if err := os.RemoveAll(b.dbPath); err != nil {
		return err
	}
	_, err := b.copyDatabaseFiles(tempPath, b.dbPath)
	return err
}

// isValidBackup checks if a backup is valid
func (b *BackupManager) isValidBackup(backupPath string) bool {
	// Check if backup exists
	if _, err := os.Stat(backupPath); os.IsNotExist(err) {
		return false
	}

	// Check if it contains expected metadata
	var metaFile string
	if strings.HasSuffix(backupPath, ".tar.gz") {
		// For compressed backups, check metadata file next to tar.gz
		metaFile = backupPath + ".metadata"
	} else {
		// For directory backups, check metadata inside directory
		metaFile = filepath.Join(backupPath, ".backup_metadata")
	}

	if _, err := os.Stat(metaFile); os.IsNotExist(err) {
		return false
	}

	return true
}

// writeBackupMetadata writes backup metadata to the appropriate location
func (b *BackupManager) writeBackupMetadata(info *BackupInfo) error {
	var metaFile string
	if strings.HasSuffix(info.Path, ".tar.gz") {
		// For compressed backups, write metadata next to the tar.gz file
		metaFile = info.Path + ".metadata"
	} else {
		// For directory backups, write metadata inside the directory
		metaFile = filepath.Join(info.Path, ".backup_metadata")
	}

	content := fmt.Sprintf(`# Pebble Database Backup Metadata
# Created: %s
# Original DB: %s
# Version: %d
# Size: %d bytes
# Description: %s

BACKUP_PATH=%s
ORIGINAL_DB=%s
CREATED_AT=%s
VERSION=%d
SIZE=%d
DESCRIPTION=%s
`,
		info.CreatedAt.Format("2006-01-02 15:04:05"),
		info.OriginalDB,
		info.Version,
		info.Size,
		info.Description,
		info.Path,
		info.OriginalDB,
		info.CreatedAt.Format(time.RFC3339),
		info.Version,
		info.Size,
		info.Description,
	)

	return os.WriteFile(metaFile, []byte(content), 0644)
}

// readBackupMetadata reads backup metadata from the appropriate location
func (b *BackupManager) readBackupMetadata(backupPath string) (*BackupInfo, error) {
	var metaFile string
	if strings.HasSuffix(backupPath, ".tar.gz") {
		// For compressed backups, read metadata from file next to tar.gz
		metaFile = backupPath + ".metadata"
	} else {
		// For directory backups, read metadata from inside the directory
		metaFile = filepath.Join(backupPath, ".backup_metadata")
	}

	content, err := os.ReadFile(metaFile)
	if err != nil {
		return nil, err
	}

	// Parse metadata (simple key=value format)
	info := &BackupInfo{Path: backupPath}

	lines := strings.Split(string(content), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "#") || strings.TrimSpace(line) == "" {
			continue
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		switch key {
		case "ORIGINAL_DB":
			info.OriginalDB = value
		case "CREATED_AT":
			if t, err := time.Parse(time.RFC3339, value); err == nil {
				info.CreatedAt = t
			}
		case "VERSION":
			fmt.Sscanf(value, "%d", &info.Version)
		case "SIZE":
			fmt.Sscanf(value, "%d", &info.Size)
		case "DESCRIPTION":
			info.Description = value
		}
	}

	return info, nil
}

// GetBackupSize calculates the size of a backup directory or file
func (b *BackupManager) GetBackupSize(backupPath string) (int64, error) {
	info, err := os.Stat(backupPath)
	if err != nil {
		return 0, err
	}

	if !info.IsDir() {
		// Single file (compressed backup)
		return info.Size(), nil
	}

	// Directory (uncompressed backup)
	var size int64
	err = filepath.Walk(backupPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			size += info.Size()
		}
		return nil
	})

	return size, err
}

// createCheckpointBackup creates an uncompressed directory backup using Pebble Checkpoint
func (b *BackupManager) createCheckpointBackup(db *pebble.DB, backupPath string) (int64, error) {
	// Create backup directory
	if err := os.MkdirAll(backupPath, 0755); err != nil {
		return 0, fmt.Errorf("failed to create backup directory: %w", err)
	}

	// Create checkpoint with flushed WAL for consistency
	if err := db.Checkpoint(backupPath, pebble.WithFlushedWAL()); err != nil {
		// Clean up failed backup
		os.RemoveAll(backupPath)
		return 0, fmt.Errorf("failed to create checkpoint: %w", err)
	}

	// Calculate total size of backup
	size, err := b.GetBackupSize(backupPath)
	if err != nil {
		return 0, fmt.Errorf("failed to calculate backup size: %w", err)
	}

	return size, nil
}

// createCompressedCheckpointBackup creates a tar.gz backup using Pebble Checkpoint
func (b *BackupManager) createCompressedCheckpointBackup(db *pebble.DB, backupPath string) (int64, error) {
	// Create temporary checkpoint directory path
	tempCheckpointPath := backupPath + ".tmp_checkpoint"
	// Clean up any existing temp directory first
	os.RemoveAll(tempCheckpointPath)
	defer os.RemoveAll(tempCheckpointPath) // Always cleanup temp directory

	// Create checkpoint with flushed WAL for consistency
	// Pebble will create the directory, so we don't use MkdirAll
	if err := db.Checkpoint(tempCheckpointPath, pebble.WithFlushedWAL()); err != nil {
		return 0, fmt.Errorf("failed to create checkpoint: %w", err)
	}

	// Create compressed archive from checkpoint
	size, err := b.compressCheckpoint(tempCheckpointPath, backupPath)
	if err != nil {
		os.Remove(backupPath) // Clean up failed backup file
		return 0, fmt.Errorf("failed to compress checkpoint: %w", err)
	}

	return size, nil
}

// compressCheckpoint compresses a checkpoint directory into a tar.gz file
func (b *BackupManager) compressCheckpoint(checkpointPath, backupPath string) (int64, error) {
	// Create the tar.gz file
	file, err := os.Create(backupPath)
	if err != nil {
		return 0, err
	}
	defer file.Close()

	// Create gzip writer
	gzipWriter := gzip.NewWriter(file)
	defer gzipWriter.Close()

	// Create tar writer
	tarWriter := tar.NewWriter(gzipWriter)
	defer tarWriter.Close()

	// Add checkpoint files to the archive
	err = filepath.Walk(checkpointPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip directories
		if info.IsDir() {
			return nil
		}

		// Create tar header
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}

		// Set relative path - use database name as root in archive
		relPath, err := filepath.Rel(checkpointPath, path)
		if err != nil {
			return err
		}
		dbName := filepath.Base(b.dbPath)
		header.Name = filepath.Join(dbName, relPath)

		// Write header
		if err := tarWriter.WriteHeader(header); err != nil {
			return err
		}

		// Copy file content
		srcFile, err := os.Open(path)
		if err != nil {
			return err
		}
		defer srcFile.Close()

		_, err = io.Copy(tarWriter, srcFile)
		return err
	})

	if err != nil {
		os.Remove(backupPath)
		return 0, err
	}

	// Get final compressed size
	stat, err := os.Stat(backupPath)
	if err != nil {
		return 0, err
	}

	return stat.Size(), nil
}

// createDirectoryBackup creates an uncompressed directory backup
func (b *BackupManager) createDirectoryBackup(backupPath string) (int64, error) {
	// Create backup directory
	if err := os.MkdirAll(backupPath, 0755); err != nil {
		return 0, fmt.Errorf("failed to create backup directory: %w", err)
	}

	// Copy database files
	size, err := b.copyDatabaseFiles(b.dbPath, backupPath)
	if err != nil {
		// Clean up failed backup
		os.RemoveAll(backupPath)
		return 0, fmt.Errorf("failed to copy database files: %w", err)
	}

	return size, nil
}

// performBackupCleanup removes old backups beyond the maxBackups limit
func (b *BackupManager) performBackupCleanup() error {
	if b.maxBackups <= 0 {
		return nil // No limit
	}

	// Find all backup files/directories for this database
	parentDir := filepath.Dir(b.dbPath)
	dbName := filepath.Base(b.dbPath)

	var backups []backupFileInfo

	entries, err := os.ReadDir(parentDir)
	if err != nil {
		return fmt.Errorf("failed to read directory: %w", err)
	}

	for _, entry := range entries {
		name := entry.Name()
		// Match backup files: dbname.backup_TIMESTAMP or dbname.backup_TIMESTAMP.tar.gz
		if strings.HasPrefix(name, dbName+".backup_") {
			fullPath := filepath.Join(parentDir, name)
			info, err := entry.Info()
			if err != nil {
				continue
			}

			backups = append(backups, backupFileInfo{
				path:    fullPath,
				modTime: info.ModTime(),
			})
		}
	}

	// Sort by modification time (newest first)
	for i := 0; i < len(backups)-1; i++ {
		for j := i + 1; j < len(backups); j++ {
			if backups[i].modTime.Before(backups[j].modTime) {
				backups[i], backups[j] = backups[j], backups[i]
			}
		}
	}

	// Remove old backups
	if len(backups) > b.maxBackups {
		for i := b.maxBackups; i < len(backups); i++ {
			fmt.Printf("Removing old backup: %s\n", backups[i].path)
			if err := os.RemoveAll(backups[i].path); err != nil {
				fmt.Printf("Warning: failed to remove backup %s: %v\n", backups[i].path, err)
			}
		}
	}

	return nil
}

// backupFileInfo holds backup file information for sorting
type backupFileInfo struct {
	path    string
	modTime time.Time
}
