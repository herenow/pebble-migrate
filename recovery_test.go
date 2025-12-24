package migrate

import (
	"strings"
	"testing"

	"github.com/cockroachdb/pebble"
)

func TestMigrationRecovery(t *testing.T) {
	// Save and restore global registry
	originalRegistry := GlobalRegistry
	defer func() {
		GlobalRegistry = originalRegistry
	}()

	t.Run("RecoverFromInterruptedRerunnableMigration", func(t *testing.T) {
		// Reset global registry for this test
		GlobalRegistry = NewMigrationRegistry()

		// Setup test database
		dir := t.TempDir()
		db, err := pebble.Open(dir, &pebble.Options{})
		if err != nil {
			t.Fatalf("Failed to open database: %v", err)
		}
		defer db.Close()

		// Track if migration was called
		migrationCalled := 0

		// Register a rerunnable migration that tracks calls
		err = GlobalRegistry.Register(&Migration{
			ID:          "1755000000_test_rerunnable",
			Description: "Test rerunnable migration",
			Up: func(db *pebble.DB) error {
				migrationCalled++
				// Simulate a migration that can be safely rerun
				return nil
			},
			Down: func(db *pebble.DB) error {
				return nil
			},
			Validate: func(db *pebble.DB) error {
				return nil
			},
			Rerunnable: true,
		})
		if err != nil {
			t.Fatalf("Failed to register migration: %v", err)
		}

		// Create schema manager
		schemaManager := NewSchemaManager(db)

		// Simulate an interrupted migration by setting status to "migrating"
		schema := &SchemaVersion{
			CurrentVersion:    0,
			AppliedMigrations: make(map[string]bool),
			MigrationHistory:  []MigrationRecord{},
			Status:            StatusMigrating, // Simulating stuck state
		}
		err = schemaManager.SetSchemaVersion(schema)
		if err != nil {
			t.Fatalf("Failed to set schema version: %v", err)
		}

		// Attempt recovery
		opts := DefaultStartupOptions()
		opts.RunMigrations = true

		// Call CheckAndRunStartupMigrations which should recover and run the migration
		err = CheckAndRunStartupMigrations(db, dir, opts)
		if err != nil {
			t.Fatalf("CheckAndRunStartupMigrations failed: %v", err)
		}

		// Verify migration was executed
		if migrationCalled != 1 {
			t.Errorf("Expected migration to be called once, but was called %d times", migrationCalled)
		}

		// Verify schema is now clean
		finalSchema, err := schemaManager.GetSchemaVersion()
		if err != nil {
			t.Fatalf("Failed to get final schema: %v", err)
		}

		if finalSchema.Status != StatusClean {
			t.Errorf("Expected status to be 'clean', got '%s'", finalSchema.Status)
		}

		if !finalSchema.AppliedMigrations["1755000000_test_rerunnable"] {
			t.Error("Expected migration to be marked as applied")
		}
	})

	t.Run("FailOnNonRerunnableMigration", func(t *testing.T) {
		// Reset global registry for this test
		GlobalRegistry = NewMigrationRegistry()

		// Setup test database
		dir := t.TempDir()
		db, err := pebble.Open(dir, &pebble.Options{})
		if err != nil {
			t.Fatalf("Failed to open database: %v", err)
		}
		defer db.Close()

		// Register a NON-rerunnable migration
		err = GlobalRegistry.Register(&Migration{
			ID:          "1755000000_test_not_rerunnable",
			Description: "Test non-rerunnable migration",
			Up: func(db *pebble.DB) error {
				return nil
			},
			Down: func(db *pebble.DB) error {
				return nil
			},
			Validate: func(db *pebble.DB) error {
				return nil
			},
			Rerunnable: false, // NOT rerunnable
		})
		if err != nil {
			t.Fatalf("Failed to register migration: %v", err)
		}

		// Create schema manager
		schemaManager := NewSchemaManager(db)

		// Simulate an interrupted migration by setting status to "migrating"
		schema := &SchemaVersion{
			CurrentVersion:    0,
			AppliedMigrations: make(map[string]bool),
			MigrationHistory:  []MigrationRecord{},
			Status:            StatusMigrating, // Simulating stuck state
		}
		err = schemaManager.SetSchemaVersion(schema)
		if err != nil {
			t.Fatalf("Failed to set schema version: %v", err)
		}

		// Attempt recovery
		opts := DefaultStartupOptions()
		opts.RunMigrations = true

		// This should fail because the migration is not rerunnable
		err = CheckAndRunStartupMigrations(db, dir, opts)
		if err == nil {
			t.Fatal("Expected error for non-rerunnable migration, but got none")
		}

		// Verify error message contains expected guidance
		if !strings.Contains(err.Error(), "not marked as rerunnable") {
			t.Errorf("Expected error to mention migration is not rerunnable, got: %v", err)
		}

		// Verify schema is still in migrating state (no auto-recovery)
		finalSchema, err := schemaManager.GetSchemaVersion()
		if err != nil {
			t.Fatalf("Failed to get final schema: %v", err)
		}

		if finalSchema.Status != StatusMigrating {
			t.Errorf("Expected status to remain 'migrating', got '%s'", finalSchema.Status)
		}
	})

	t.Run("RecoverWithNoMigrationsApplied", func(t *testing.T) {
		// Test case where database is stuck with ZERO migrations completed
		// This simulates the scenario where the very first migration was interrupted

		// Reset global registry for this test
		GlobalRegistry = NewMigrationRegistry()

		dir := t.TempDir()
		db, err := pebble.Open(dir, &pebble.Options{})
		if err != nil {
			t.Fatalf("Failed to open database: %v", err)
		}
		defer db.Close()

		// Track if migration was called
		migrationCalled := 0

		// Register the first migration as rerunnable
		err = GlobalRegistry.Register(&Migration{
			ID:          "1755003600_initial_migration",
			Description: "Initial migration",
			Up: func(db *pebble.DB) error {
				migrationCalled++
				return nil
			},
			Down: func(db *pebble.DB) error {
				return nil
			},
			Validate: func(db *pebble.DB) error {
				return nil
			},
			Rerunnable: true,
		})
		if err != nil {
			t.Fatalf("Failed to register migration: %v", err)
		}

		// Create schema manager
		schemaManager := NewSchemaManager(db)

		// Set schema to migrating with NO applied migrations
		schema := &SchemaVersion{
			CurrentVersion:    0,
			AppliedMigrations: map[string]bool{}, // Empty - no migrations applied
			MigrationHistory:  []MigrationRecord{},
			Status:            StatusMigrating, // Stuck on first migration
		}
		err = schemaManager.SetSchemaVersion(schema)
		if err != nil {
			t.Fatalf("Failed to set schema version: %v", err)
		}

		// Attempt recovery
		opts := DefaultStartupOptions()
		opts.RunMigrations = true

		// Should recover and run the first migration
		err = CheckAndRunStartupMigrations(db, dir, opts)
		if err != nil {
			t.Fatalf("CheckAndRunStartupMigrations failed: %v", err)
		}

		// Verify migration was executed
		if migrationCalled != 1 {
			t.Errorf("Expected first migration to be called once, but was called %d times", migrationCalled)
		}

		// Verify schema is now clean with the migration applied
		finalSchema, err := schemaManager.GetSchemaVersion()
		if err != nil {
			t.Fatalf("Failed to get final schema: %v", err)
		}

		if finalSchema.Status != StatusClean {
			t.Errorf("Expected status to be 'clean', got '%s'", finalSchema.Status)
		}

		if !finalSchema.AppliedMigrations["1755003600_initial_migration"] {
			t.Error("Expected initial migration to be marked as applied")
		}

		if finalSchema.CurrentVersion != 1755003600 {
			t.Errorf("Expected current version to be 1755003600, got %d", finalSchema.CurrentVersion)
		}
	})
}
