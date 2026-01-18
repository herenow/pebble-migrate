package migrate

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/cockroachdb/pebble"
)

// SchemaManager handles schema version management in Pebble
type SchemaManager struct {
	db *pebble.DB
}

// NewSchemaManager creates a new schema manager
func NewSchemaManager(db *pebble.DB) *SchemaManager {
	return &SchemaManager{
		db: db,
	}
}

// GetSchemaVersion retrieves the current schema version from Pebble
func (s *SchemaManager) GetSchemaVersion() (*SchemaVersion, error) {
	data, closer, err := s.db.Get([]byte(SchemaVersionKey))
	if err != nil {
		if err == pebble.ErrNotFound {
			// Return default schema version for new databases
			return &SchemaVersion{
				CurrentVersion:    0,
				AppliedMigrations: make(map[string]bool),
				MigrationHistory:  make([]MigrationRecord, 0),
				LastMigrationAt:   time.Time{},
				Status:            StatusClean,
			}, nil
		}
		return nil, fmt.Errorf("failed to get schema version: %w", err)
	}
	defer closer.Close()

	var version SchemaVersion
	if err := json.Unmarshal(data, &version); err != nil {
		return nil, fmt.Errorf("failed to unmarshal schema version: %w", err)
	}

	return &version, nil
}

// SetSchemaVersion stores the schema version in Pebble
func (s *SchemaManager) SetSchemaVersion(version *SchemaVersion) error {
	data, err := json.Marshal(version)
	if err != nil {
		return fmt.Errorf("failed to marshal schema version: %w", err)
	}

	if err := s.db.Set([]byte(SchemaVersionKey), data, pebble.Sync); err != nil {
		return fmt.Errorf("failed to store schema version: %w", err)
	}

	return nil
}

// UpdateSchemaAfterMigration updates the schema after a successful migration
func (s *SchemaManager) UpdateSchemaAfterMigration(migrationID string, version int64, description string, duration time.Duration) error {
	currentSchema, err := s.GetSchemaVersion()
	if err != nil {
		return fmt.Errorf("failed to get current schema version: %w", err)
	}

	// Add migration record
	record := MigrationRecord{
		ID:          migrationID,
		Description: description,
		AppliedAt:   time.Now(),
		Duration:    duration.String(),
		Success:     true,
	}

	// Mark migration as applied
	if currentSchema.AppliedMigrations == nil {
		currentSchema.AppliedMigrations = make(map[string]bool)
	}
	currentSchema.AppliedMigrations[migrationID] = true
	currentSchema.MigrationHistory = append(currentSchema.MigrationHistory, record)
	currentSchema.LastMigrationAt = record.AppliedAt
	currentSchema.Status = StatusClean

	// Update current version to the migration's Unix timestamp
	if version > currentSchema.CurrentVersion {
		currentSchema.CurrentVersion = version
	}

	return s.SetSchemaVersion(currentSchema)
}

// MarkMigrationStarted marks the beginning of a migration
func (s *SchemaManager) MarkMigrationStarted() error {
	currentSchema, err := s.GetSchemaVersion()
	if err != nil {
		return fmt.Errorf("failed to get current schema version: %w", err)
	}

	currentSchema.Status = StatusMigrating
	return s.SetSchemaVersion(currentSchema)
}

// MarkMigrationFailed marks a migration as failed
func (s *SchemaManager) MarkMigrationFailed(migrationID string, description string, migrationErr error) error {
	currentSchema, err := s.GetSchemaVersion()
	if err != nil {
		return fmt.Errorf("failed to get current schema: %w", err)
	}

	// Add failed migration record to history
	record := MigrationRecord{
		ID:          migrationID,
		Description: description + " (FAILED)",
		AppliedAt:   time.Now(),
		Duration:    "0s",
		Success:     false,
		Error:       migrationErr.Error(),
	}

	currentSchema.MigrationHistory = append(currentSchema.MigrationHistory, record)
	currentSchema.LastMigrationAt = record.AppliedAt
	currentSchema.Status = StatusDirty

	return s.SetSchemaVersion(currentSchema)
}

// MarkRollbackStarted marks the beginning of a rollback
func (s *SchemaManager) MarkRollbackStarted() error {
	currentSchema, err := s.GetSchemaVersion()
	if err != nil {
		return fmt.Errorf("failed to get current schema version: %w", err)
	}

	currentSchema.Status = StatusRollback
	return s.SetSchemaVersion(currentSchema)
}

// UpdateAfterRollback updates the schema after a successful rollback
func (s *SchemaManager) UpdateAfterRollback(migrationID string, version int64, description string) error {
	currentSchema, err := s.GetSchemaVersion()
	if err != nil {
		return fmt.Errorf("failed to get current schema: %w", err)
	}

	// Remove the migration from applied set
	if currentSchema.AppliedMigrations != nil {
		delete(currentSchema.AppliedMigrations, migrationID)
	}

	// Add rollback record to history
	rollbackRecord := MigrationRecord{
		ID:          migrationID + "_rollback",
		Description: fmt.Sprintf("Rolled back: %s", description),
		AppliedAt:   time.Now(),
		Duration:    "0s",
		Success:     true,
	}

	currentSchema.MigrationHistory = append(currentSchema.MigrationHistory, rollbackRecord)
	currentSchema.LastMigrationAt = rollbackRecord.AppliedAt
	currentSchema.Status = StatusClean

	// Update current version after rollback
	// Find the highest version among remaining applied migrations
	var maxVersion int64 = 0
	for migID := range currentSchema.AppliedMigrations {
		if migVersion, err := ParseMigrationVersion(migID); err == nil && migVersion > maxVersion {
			maxVersion = migVersion
		}
	}
	currentSchema.CurrentVersion = maxVersion

	return s.SetSchemaVersion(currentSchema)
}

// GetMigrationHistory returns the history of applied migrations
func (s *SchemaManager) GetMigrationHistory() ([]MigrationRecord, error) {
	currentSchema, err := s.GetSchemaVersion()
	if err != nil {
		return nil, err
	}

	return currentSchema.MigrationHistory, nil
}

// IsMigrationApplied checks if a specific migration has been applied
func (s *SchemaManager) IsMigrationApplied(migrationID string) (bool, error) {
	currentSchema, err := s.GetSchemaVersion()
	if err != nil {
		return false, err
	}

	if currentSchema.AppliedMigrations == nil {
		return false, nil
	}

	return currentSchema.AppliedMigrations[migrationID], nil
}

// SetCurrentVersion sets the current version (Unix timestamp) for the repository
func (s *SchemaManager) SetCurrentVersion(version int64) error {
	currentSchema, err := s.GetSchemaVersion()
	if err != nil {
		return fmt.Errorf("failed to get current schema: %w", err)
	}

	currentSchema.CurrentVersion = version
	return s.SetSchemaVersion(currentSchema)
}

// ValidateSchemaState performs basic validation on the schema state
func (s *SchemaManager) ValidateSchemaState() error {
	currentSchema, err := s.GetSchemaVersion()
	if err != nil {
		return fmt.Errorf("failed to get schema version: %w", err)
	}

	// Check for dirty state
	if currentSchema.Status == StatusDirty {
		return fmt.Errorf("database is in dirty state, manual intervention required")
	}

	// Check for ongoing migration
	if currentSchema.Status == StatusMigrating {
		return fmt.Errorf("migration is currently in progress")
	}

	// Check for ongoing rollback
	if currentSchema.Status == StatusRollback {
		return fmt.Errorf("rollback is currently in progress")
	}

	// Validate applied migrations are consistent with history
	successfulMigrations := make(map[string]bool)
	for _, record := range currentSchema.MigrationHistory {
		if record.Success && !isRollbackRecord(record.ID) {
			successfulMigrations[record.ID] = true
		} else if isRollbackRecord(record.ID) {
			// Remove original migration from successful set if rollback succeeded
			originalID := record.ID[:len(record.ID)-9] // Remove "_rollback" suffix
			delete(successfulMigrations, originalID)
		}
	}

	// Check consistency between applied migrations and history
	if currentSchema.AppliedMigrations == nil {
		currentSchema.AppliedMigrations = make(map[string]bool)
	}

	// The applied migrations should match successful migrations from history
	for id := range successfulMigrations {
		if !currentSchema.AppliedMigrations[id] {
			return fmt.Errorf("migration %s appears in history as successful but not marked as applied", id)
		}
	}

	for id := range currentSchema.AppliedMigrations {
		if !successfulMigrations[id] {
			return fmt.Errorf("migration %s marked as applied but no successful record in history", id)
		}
	}

	return nil
}

// isRollbackRecord checks if a migration record is a rollback record
func isRollbackRecord(id string) bool {
	return len(id) > 9 && id[len(id)-9:] == "_rollback"
}

// ForceCleanState forces the schema to clean state (use with caution)
// Note: This only changes the Status field. It does NOT fix missing history records.
// Use RepairMissingHistory() to fix consistency issues between AppliedMigrations and MigrationHistory.
func (s *SchemaManager) ForceCleanState() error {
	currentSchema, err := s.GetSchemaVersion()
	if err != nil {
		return fmt.Errorf("failed to get current schema version: %w", err)
	}

	currentSchema.Status = StatusClean
	return s.SetSchemaVersion(currentSchema)
}

// RepairMissingHistory creates synthetic history records for any migrations
// that are marked as applied but don't have corresponding history entries.
// This fixes the inconsistency that causes ValidateSchemaState() to fail.
func (s *SchemaManager) RepairMissingHistory(registry *MigrationRegistry) ([]string, error) {
	currentSchema, err := s.GetSchemaVersion()
	if err != nil {
		return nil, fmt.Errorf("failed to get schema version: %w", err)
	}

	// Build set of migrations that have successful history records
	successfulInHistory := make(map[string]bool)
	for _, record := range currentSchema.MigrationHistory {
		if record.Success && !isRollbackRecord(record.ID) {
			successfulInHistory[record.ID] = true
		}
	}

	// Find applied migrations missing from history
	var repaired []string
	now := time.Now()

	for migrationID := range currentSchema.AppliedMigrations {
		if !successfulInHistory[migrationID] {
			// Look up migration description from registry
			description := "unknown migration"
			if m, ok := registry.GetMigration(migrationID); ok {
				description = m.Description
			}

			// Create synthetic history record
			currentSchema.MigrationHistory = append(currentSchema.MigrationHistory, MigrationRecord{
				ID:          migrationID,
				Description: description + " (repaired - missing history)",
				AppliedAt:   now,
				Duration:    "0s",
				Success:     true,
			})
			repaired = append(repaired, migrationID)
		}
	}

	if len(repaired) == 0 {
		return nil, nil // Nothing to repair
	}

	// Also ensure status is clean
	currentSchema.Status = StatusClean

	if err := s.SetSchemaVersion(currentSchema); err != nil {
		return nil, fmt.Errorf("failed to save repaired schema: %w", err)
	}

	return repaired, nil
}

// InitializeFreshDatabase initializes schema for databases without __schema_version.
// - If DB is empty (no keys): fresh database -> initialize at latest version
// - If DB has keys: pre-migration database -> set version 0, run migrations
func (s *SchemaManager) InitializeFreshDatabase(registry *MigrationRegistry) error {
	// Check if schema key already exists
	_, closer, err := s.db.Get([]byte(SchemaVersionKey))
	if err == nil {
		closer.Close()
		return nil // Already initialized, nothing to do
	}
	if err != pebble.ErrNotFound {
		return fmt.Errorf("failed to check schema version: %w", err)
	}

	// Schema key doesn't exist - check if DB has any data at all
	isEmpty, err := s.isDatabaseEmpty()
	if err != nil {
		return fmt.Errorf("failed to check if database is empty: %w", err)
	}

	if !isEmpty {
		// Pre-migration-system database (has data but no schema version)
		// Set version 0 so all migrations will run
		return s.SetSchemaVersion(&SchemaVersion{
			CurrentVersion:    0,
			AppliedMigrations: make(map[string]bool),
			MigrationHistory:  make([]MigrationRecord, 0),
			Status:            StatusClean,
		})
	}

	// Truly fresh database - initialize at latest version
	migrations := registry.GetMigrations()
	if len(migrations) == 0 {
		return s.SetSchemaVersion(&SchemaVersion{
			CurrentVersion:    0,
			AppliedMigrations: make(map[string]bool),
			MigrationHistory:  make([]MigrationRecord, 0),
			Status:            StatusClean,
		})
	}

	// Find max version and mark all as applied WITH history records
	var maxVersion int64
	appliedMigrations := make(map[string]bool)
	migrationHistory := make([]MigrationRecord, 0, len(migrations))
	now := time.Now()

	for _, m := range migrations {
		if m.Version > maxVersion {
			maxVersion = m.Version
		}
		appliedMigrations[m.ID] = true

		// Create synthetic history record for fresh db initialization
		migrationHistory = append(migrationHistory, MigrationRecord{
			ID:          m.ID,
			Description: m.Description + " (skipped - fresh database)",
			AppliedAt:   now,
			Duration:    "0s",
			Success:     true,
		})
	}

	return s.SetSchemaVersion(&SchemaVersion{
		CurrentVersion:    maxVersion,
		AppliedMigrations: appliedMigrations,
		MigrationHistory:  migrationHistory,
		Status:            StatusClean,
	})
}

// isDatabaseEmpty checks if the database has any keys at all
func (s *SchemaManager) isDatabaseEmpty() (bool, error) {
	iter, err := s.db.NewIter(nil) // nil options = iterate all keys
	if err != nil {
		return false, err
	}
	defer iter.Close()

	// If First() returns false, there are no keys
	return !iter.First(), nil
}
