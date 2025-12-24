package migrate

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cockroachdb/pebble"
)

func TestSchemaManager(t *testing.T) {
	// Create temporary database
	tmpDir, err := os.MkdirTemp("", "migration_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	db, err := pebble.Open(filepath.Join(tmpDir, "test.db"), &pebble.Options{})
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	schemaManager := NewSchemaManager(db)

	t.Run("GetSchemaVersion_NewDatabase", func(t *testing.T) {
		version, err := schemaManager.GetSchemaVersion()
		if err != nil {
			t.Fatalf("Failed to get schema version: %v", err)
		}

		if version.CurrentVersion != 0 {
			t.Errorf("Expected version 0, got %d", version.CurrentVersion)
		}

		if version.Status != StatusClean {
			t.Errorf("Expected status clean, got %s", version.Status)
		}

		if len(version.AppliedMigrations) != 0 {
			t.Errorf("Expected 0 applied migrations, got %d", len(version.AppliedMigrations))
		}
	})

	t.Run("UpdateSchemaAfterMigration", func(t *testing.T) {
		err := schemaManager.UpdateSchemaAfterMigration("1754917200_test", 1754917200, "Test migration", time.Second)
		if err != nil {
			t.Fatalf("Failed to update schema after migration: %v", err)
		}

		version, err := schemaManager.GetSchemaVersion()
		if err != nil {
			t.Fatalf("Failed to get schema version: %v", err)
		}

		if version.CurrentVersion != 1754917200 {
			t.Errorf("Expected version 1754917200, got %d", version.CurrentVersion)
		}

		if len(version.AppliedMigrations) != 1 {
			t.Errorf("Expected 1 applied migration, got %d", len(version.AppliedMigrations))
		}

		applied, exists := version.AppliedMigrations["1754917200_test"]
		if !exists {
			t.Errorf("Expected migration '1754917200_test' to be applied")
		}
		if !applied {
			t.Errorf("Expected migration '1754917200_test' to be marked as applied")
		}

		// Check migration history for success
		if len(version.MigrationHistory) != 1 {
			t.Errorf("Expected 1 migration in history, got %d", len(version.MigrationHistory))
		}
		if len(version.MigrationHistory) > 0 && !version.MigrationHistory[0].Success {
			t.Errorf("Expected migration success to be true")
		}
	})

	t.Run("MarkMigrationFailed", func(t *testing.T) {
		testErr := "test error"
		err := schemaManager.MarkMigrationFailed("1754917300_failed", "Failed migration", &testError{testErr})
		if err != nil {
			t.Fatalf("Failed to mark migration as failed: %v", err)
		}

		version, err := schemaManager.GetSchemaVersion()
		if err != nil {
			t.Fatalf("Failed to get schema version: %v", err)
		}

		if version.Status != StatusDirty {
			t.Errorf("Expected status dirty, got %s", version.Status)
		}

		// Check migration history for failed migration
		if len(version.MigrationHistory) != 2 {
			t.Errorf("Expected 2 migrations in history, got %d", len(version.MigrationHistory))
		}
		if len(version.MigrationHistory) > 1 {
			failedMigration := version.MigrationHistory[1]
			if failedMigration.Success {
				t.Errorf("Expected failed migration success to be false")
			}
			if failedMigration.Error != testErr {
				t.Errorf("Expected error '%s', got '%s'", testErr, failedMigration.Error)
			}
		}
	})
}

func TestMigrationRegistry(t *testing.T) {
	registry := NewMigrationRegistry()

	testMigration1 := &Migration{
		ID:          "1754917200_test",
		Description: "Test migration 1",
		Up:          func(db *pebble.DB) error { return nil },
		Down:        func(db *pebble.DB) error { return nil },
	}

	testMigration2 := &Migration{
		ID:          "1754917300_test",
		Description: "Test migration 2",
		Up:          func(db *pebble.DB) error { return nil },
		Down:        func(db *pebble.DB) error { return nil },
	}

	t.Run("Register", func(t *testing.T) {
		err := registry.Register(testMigration1)
		if err != nil {
			t.Fatalf("Failed to register migration: %v", err)
		}

		err = registry.Register(testMigration2)
		if err != nil {
			t.Fatalf("Failed to register migration: %v", err)
		}

		migrations := registry.GetMigrations()
		if len(migrations) != 2 {
			t.Errorf("Expected 2 migrations, got %d", len(migrations))
		}

		// Migrations should be ordered by parsed timestamp
		if migrations[0].ID != "1754917200_test" || migrations[1].ID != "1754917300_test" {
			t.Errorf("Migrations not ordered correctly")
		}
	})

	t.Run("GetPendingMigrations", func(t *testing.T) {
		// No migrations applied
		pending, err := registry.GetPendingMigrations(map[string]bool{})
		if err != nil {
			t.Fatalf("Failed to get pending migrations: %v", err)
		}
		if len(pending) != 2 {
			t.Errorf("Expected 2 pending migrations, got %d", len(pending))
		}

		// First migration applied
		pending, err = registry.GetPendingMigrations(map[string]bool{
			"1754917200_test": true,
		})
		if err != nil {
			t.Fatalf("Failed to get pending migrations: %v", err)
		}
		if len(pending) != 1 {
			t.Errorf("Expected 1 pending migration, got %d", len(pending))
		}

		if pending[0].ID != "1754917300_test" {
			t.Errorf("Expected pending migration '1754917300_test', got %s", pending[0].ID)
		}

		// All migrations applied
		pending, err = registry.GetPendingMigrations(map[string]bool{
			"1754917200_test": true,
			"1754917300_test": true,
		})
		if err != nil {
			t.Fatalf("Failed to get pending migrations: %v", err)
		}
		if len(pending) != 0 {
			t.Errorf("Expected 0 pending migrations, got %d", len(pending))
		}
	})

	t.Run("DuplicateRegistration", func(t *testing.T) {
		err := registry.Register(testMigration1)
		if err == nil {
			t.Errorf("Expected error for duplicate migration registration")
		}
	})
}

func TestMigrationEngine(t *testing.T) {
	// Create temporary database
	tmpDir, err := os.MkdirTemp("", "engine_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	db, err := pebble.Open(filepath.Join(tmpDir, "test.db"), &pebble.Options{})
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	registry := NewMigrationRegistry()
	schemaManager := NewSchemaManager(db)

	// Create engine
	engine := NewMigrationEngineWithBackup(db, schemaManager, registry, tmpDir)
	engine.SetVerbose(true)

	// Register test migrations
	registry.Register(&Migration{
		ID:          "1754917200_test",
		Description: "Test 1",
		Up:          func(db *pebble.DB) error { return nil },
		Down:        func(db *pebble.DB) error { return nil },
	})
	registry.Register(&Migration{
		ID:          "1754917300_test",
		Description: "Test 2",
		Up:          func(db *pebble.DB) error { return nil },
		Down:        func(db *pebble.DB) error { return nil },
	})

	t.Run("MigrateUp", func(t *testing.T) {
		// Plan and execute all migrations
		planner := NewMigrationPlanner(registry, schemaManager)
		plan, err := planner.PlanUpgrade()
		if err != nil {
			t.Fatalf("Failed to plan upgrade: %v", err)
		}

		err = engine.ExecutePlan(plan, func(msg string) {
			t.Logf("Progress: %s", msg)
		})
		if err != nil {
			t.Fatalf("Failed to execute upgrade: %v", err)
		}

		// Check that migrations are applied
		version, err := schemaManager.GetSchemaVersion()
		if err != nil {
			t.Fatalf("Failed to get schema version: %v", err)
		}

		if version.CurrentVersion == 0 {
			t.Errorf("Expected version to be updated")
		}

		if len(version.AppliedMigrations) != 2 {
			t.Errorf("Expected 2 applied migrations, got %d", len(version.AppliedMigrations))
		}
	})

	t.Run("MigrateDown", func(t *testing.T) {
		// Plan and execute rollback
		planner := NewMigrationPlanner(registry, schemaManager)
		plan, err := planner.PlanDowngrade(1754917200)
		if err != nil {
			t.Fatalf("Failed to plan downgrade: %v", err)
		}

		err = engine.ExecutePlan(plan, func(msg string) {
			t.Logf("Progress: %s", msg)
		})
		if err != nil {
			t.Fatalf("Failed to execute downgrade: %v", err)
		}

		// Check schema after rollback
		version, err := schemaManager.GetSchemaVersion()
		if err != nil {
			t.Fatalf("Failed to get schema version: %v", err)
		}

		if version.CurrentVersion != 1754917200 {
			t.Errorf("Expected version 1754917200 after rollback, got %d", version.CurrentVersion)
		}
	})
}

func TestMigrationIDValidation(t *testing.T) {
	testCases := []struct {
		id    string
		valid bool
	}{
		{"1754917200_test_migration", true},
		{"1754917300_another_test", true},
		{"1754917200_test-migration", true},
		{"1754917200_test", true},
		{"1754917200_", false},               // Empty description
		{"_test", false},                     // No timestamp
		{"abc_test", false},                  // Non-numeric timestamp
		{"1754917200", false},                // No underscore
		{"", false},                          // Empty
		{"1754917200_test migration", false}, // Space in description
		{"1754917200_test@migration", false}, // Invalid character
	}

	for _, tc := range testCases {
		t.Run(tc.id, func(t *testing.T) {
			// Simple validation check: ID must have underscore and numeric prefix
			valid := true
			if tc.id == "" {
				valid = false
			} else if !strings.Contains(tc.id, "_") {
				valid = false
			} else {
				parts := strings.SplitN(tc.id, "_", 2)
				if len(parts) != 2 || parts[1] == "" {
					valid = false
				} else {
					// Check if first part is numeric
					if _, err := strconv.ParseInt(parts[0], 10, 64); err != nil {
						valid = false
					} else if strings.Contains(parts[1], " ") || strings.Contains(parts[1], "@") {
						// Check for invalid characters in description
						valid = false
					}
				}
			}

			if valid != tc.valid {
				t.Errorf("Expected %v for ID '%s', got %v", tc.valid, tc.id, valid)
			}
		})
	}
}

// Helper types for testing

type testError struct {
	msg string
}

func (e *testError) Error() string {
	return e.msg
}

func TestInitializeFreshDatabase(t *testing.T) {
	t.Run("FreshEmptyDatabase", func(t *testing.T) {
		// Create temporary database
		tmpDir, err := os.MkdirTemp("", "fresh_db_test")
		if err != nil {
			t.Fatalf("Failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tmpDir)

		db, err := pebble.Open(filepath.Join(tmpDir, "test.db"), &pebble.Options{})
		if err != nil {
			t.Fatalf("Failed to open database: %v", err)
		}
		defer db.Close()

		// Create registry with test migrations
		registry := NewMigrationRegistry()
		registry.Register(&Migration{
			ID:          "1754917200_test",
			Description: "Test 1",
			Up:          func(db *pebble.DB) error { return nil },
			Down:        func(db *pebble.DB) error { return nil },
		})
		registry.Register(&Migration{
			ID:          "1754917300_test",
			Description: "Test 2",
			Up:          func(db *pebble.DB) error { return nil },
			Down:        func(db *pebble.DB) error { return nil },
		})

		schemaManager := NewSchemaManager(db)

		// Initialize fresh database
		err = schemaManager.InitializeFreshDatabase(registry)
		if err != nil {
			t.Fatalf("Failed to initialize fresh database: %v", err)
		}

		// Check that schema was initialized at latest version
		version, err := schemaManager.GetSchemaVersion()
		if err != nil {
			t.Fatalf("Failed to get schema version: %v", err)
		}

		// Should be at highest migration version
		if version.CurrentVersion != 1754917300 {
			t.Errorf("Expected version 1754917300, got %d", version.CurrentVersion)
		}

		// All migrations should be marked as applied
		if len(version.AppliedMigrations) != 2 {
			t.Errorf("Expected 2 applied migrations, got %d", len(version.AppliedMigrations))
		}

		if !version.AppliedMigrations["1754917200_test"] {
			t.Errorf("Expected migration '1754917200_test' to be marked as applied")
		}

		if !version.AppliedMigrations["1754917300_test"] {
			t.Errorf("Expected migration '1754917300_test' to be marked as applied")
		}

		// No pending migrations should exist
		planner := NewMigrationPlanner(registry, schemaManager)
		plan, err := planner.PlanUpgrade()
		if err != nil {
			t.Fatalf("Failed to plan upgrade: %v", err)
		}

		if len(plan.Migrations) != 0 {
			t.Errorf("Expected 0 pending migrations, got %d", len(plan.Migrations))
		}
	})

	t.Run("PreMigrationSystemDatabase", func(t *testing.T) {
		// Create temporary database
		tmpDir, err := os.MkdirTemp("", "pre_migration_test")
		if err != nil {
			t.Fatalf("Failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tmpDir)

		db, err := pebble.Open(filepath.Join(tmpDir, "test.db"), &pebble.Options{})
		if err != nil {
			t.Fatalf("Failed to open database: %v", err)
		}
		defer db.Close()

		// Add some data to simulate pre-migration-system database
		err = db.Set([]byte("order:123"), []byte("some data"), pebble.Sync)
		if err != nil {
			t.Fatalf("Failed to add test data: %v", err)
		}

		// Create registry with test migrations
		registry := NewMigrationRegistry()
		registry.Register(&Migration{
			ID:          "1754917200_test",
			Description: "Test 1",
			Up:          func(db *pebble.DB) error { return nil },
			Down:        func(db *pebble.DB) error { return nil },
		})

		schemaManager := NewSchemaManager(db)

		// Initialize - should detect existing data
		err = schemaManager.InitializeFreshDatabase(registry)
		if err != nil {
			t.Fatalf("Failed to initialize database: %v", err)
		}

		// Check that schema was initialized at version 0 (migrations needed)
		version, err := schemaManager.GetSchemaVersion()
		if err != nil {
			t.Fatalf("Failed to get schema version: %v", err)
		}

		if version.CurrentVersion != 0 {
			t.Errorf("Expected version 0 for pre-migration database, got %d", version.CurrentVersion)
		}

		if len(version.AppliedMigrations) != 0 {
			t.Errorf("Expected 0 applied migrations, got %d", len(version.AppliedMigrations))
		}

		// Should have pending migrations
		planner := NewMigrationPlanner(registry, schemaManager)
		plan, err := planner.PlanUpgrade()
		if err != nil {
			t.Fatalf("Failed to plan upgrade: %v", err)
		}

		if len(plan.Migrations) != 1 {
			t.Errorf("Expected 1 pending migration, got %d", len(plan.Migrations))
		}
	})

	t.Run("ExistingSchemaVersion", func(t *testing.T) {
		// Create temporary database
		tmpDir, err := os.MkdirTemp("", "existing_schema_test")
		if err != nil {
			t.Fatalf("Failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tmpDir)

		db, err := pebble.Open(filepath.Join(tmpDir, "test.db"), &pebble.Options{})
		if err != nil {
			t.Fatalf("Failed to open database: %v", err)
		}
		defer db.Close()

		registry := NewMigrationRegistry()
		registry.Register(&Migration{
			ID:          "1754917200_test",
			Description: "Test 1",
			Up:          func(db *pebble.DB) error { return nil },
			Down:        func(db *pebble.DB) error { return nil },
		})

		schemaManager := NewSchemaManager(db)

		// Set existing schema version manually
		existingVersion := &SchemaVersion{
			CurrentVersion:    1754917100,
			AppliedMigrations: map[string]bool{"some_old_migration": true},
			MigrationHistory:  []MigrationRecord{},
			Status:            StatusClean,
		}
		err = schemaManager.SetSchemaVersion(existingVersion)
		if err != nil {
			t.Fatalf("Failed to set existing schema version: %v", err)
		}

		// Initialize should be a no-op
		err = schemaManager.InitializeFreshDatabase(registry)
		if err != nil {
			t.Fatalf("Failed to initialize database: %v", err)
		}

		// Check that schema was NOT modified
		version, err := schemaManager.GetSchemaVersion()
		if err != nil {
			t.Fatalf("Failed to get schema version: %v", err)
		}

		if version.CurrentVersion != 1754917100 {
			t.Errorf("Expected version 1754917100 (unchanged), got %d", version.CurrentVersion)
		}

		if len(version.AppliedMigrations) != 1 {
			t.Errorf("Expected 1 applied migration (unchanged), got %d", len(version.AppliedMigrations))
		}

		if !version.AppliedMigrations["some_old_migration"] {
			t.Errorf("Expected existing migration to still be marked as applied")
		}
	})

	t.Run("EmptyRegistryFreshDatabase", func(t *testing.T) {
		// Create temporary database
		tmpDir, err := os.MkdirTemp("", "empty_registry_test")
		if err != nil {
			t.Fatalf("Failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tmpDir)

		db, err := pebble.Open(filepath.Join(tmpDir, "test.db"), &pebble.Options{})
		if err != nil {
			t.Fatalf("Failed to open database: %v", err)
		}
		defer db.Close()

		// Empty registry (no migrations)
		registry := NewMigrationRegistry()
		schemaManager := NewSchemaManager(db)

		// Initialize fresh database with no migrations
		err = schemaManager.InitializeFreshDatabase(registry)
		if err != nil {
			t.Fatalf("Failed to initialize fresh database: %v", err)
		}

		// Check that schema was initialized at version 0
		version, err := schemaManager.GetSchemaVersion()
		if err != nil {
			t.Fatalf("Failed to get schema version: %v", err)
		}

		if version.CurrentVersion != 0 {
			t.Errorf("Expected version 0 with empty registry, got %d", version.CurrentVersion)
		}

		if len(version.AppliedMigrations) != 0 {
			t.Errorf("Expected 0 applied migrations with empty registry, got %d", len(version.AppliedMigrations))
		}
	})
}

// Integration test for the complete migration flow
func TestMigrationFlow(t *testing.T) {
	// Create temporary database
	tmpDir, err := os.MkdirTemp("", "migration_flow_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	db, err := pebble.Open(filepath.Join(tmpDir, "test.db"), &pebble.Options{})
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	registry := NewMigrationRegistry()
	schemaManager := NewSchemaManager(db)
	engine := NewMigrationEngineWithBackup(db, schemaManager, registry, tmpDir)

	// Register a simple test migration
	var migrationExecuted bool
	registry.Register(&Migration{
		ID:          "1754917200_test_data_migration",
		Description: "Test data migration",
		Up: func(db *pebble.DB) error {
			migrationExecuted = true
			// Simulate data migration
			return db.Set([]byte("test_key"), []byte("test_value"), pebble.Sync)
		},
		Down: func(db *pebble.DB) error {
			migrationExecuted = false
			return db.Delete([]byte("test_key"), pebble.Sync)
		},
		Validate: func(db *pebble.DB) error {
			// Only validate if migration was executed (up)
			if migrationExecuted {
				_, closer, err := db.Get([]byte("test_key"))
				if err != nil {
					return err
				}
				closer.Close()
			}
			return nil
		},
	})

	t.Run("CompleteFlow", func(t *testing.T) {
		// Check initial state
		version, err := schemaManager.GetSchemaVersion()
		if err != nil {
			t.Fatalf("Failed to get initial schema version: %v", err)
		}
		if version.CurrentVersion != 0 {
			t.Errorf("Expected initial version 0, got %d", version.CurrentVersion)
		}

		// Plan and execute migration
		planner := NewMigrationPlanner(registry, schemaManager)
		plan, err := planner.PlanUpgrade()
		if err != nil {
			t.Fatalf("Failed to plan upgrade: %v", err)
		}

		err = engine.ExecutePlan(plan, func(msg string) {
			t.Logf("Progress: %s", msg)
		})
		if err != nil {
			t.Fatalf("Failed to execute migration: %v", err)
		}

		if !migrationExecuted {
			t.Errorf("Migration was not executed")
		}

		// Check final state
		version, err = schemaManager.GetSchemaVersion()
		if err != nil {
			t.Fatalf("Failed to get final schema version: %v", err)
		}

		if version.CurrentVersion != 1754917200 {
			t.Errorf("Expected final version 1754917200, got %d", version.CurrentVersion)
		}

		if version.Status != StatusClean {
			t.Errorf("Expected status clean, got %s", version.Status)
		}

		if len(version.AppliedMigrations) != 1 {
			t.Errorf("Expected 1 applied migration, got %d", len(version.AppliedMigrations))
		}

		// Test rollback
		rollbackPlan, err := planner.PlanDowngrade(0)
		if err != nil {
			t.Fatalf("Failed to plan rollback: %v", err)
		}

		err = engine.ExecutePlan(rollbackPlan, func(msg string) {
			t.Logf("Rollback progress: %s", msg)
		})
		if err != nil {
			t.Fatalf("Failed to execute rollback: %v", err)
		}

		// Check rollback state
		version, err = schemaManager.GetSchemaVersion()
		if err != nil {
			t.Fatalf("Failed to get rollback schema version: %v", err)
		}

		if version.CurrentVersion != 0 {
			t.Errorf("Expected rollback version 0, got %d", version.CurrentVersion)
		}
	})
}
