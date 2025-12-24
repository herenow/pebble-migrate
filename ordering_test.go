package migrate

import (
	"testing"

	"github.com/cockroachdb/pebble"
)

func TestMigrationOrdering(t *testing.T) {
	// Create a test registry
	registry := NewMigrationRegistry()

	// Register migrations with different timestamps and dependencies
	// Migration 1: Early timestamp, no dependencies
	m1 := &Migration{
		ID:           "1000000000_first",
		Description:  "First migration",
		Dependencies: []string{},
		Up:           func(db *pebble.DB) error { return nil },
		Down:         func(db *pebble.DB) error { return nil },
	}

	// Migration 2: Later timestamp, no dependencies
	m2 := &Migration{
		ID:           "2000000000_second",
		Description:  "Second migration",
		Dependencies: []string{},
		Up:           func(db *pebble.DB) error { return nil },
		Down:         func(db *pebble.DB) error { return nil },
	}

	// Migration 3: Earlier timestamp but depends on migration 2
	m3 := &Migration{
		ID:           "1500000000_third",
		Description:  "Third migration (depends on second)",
		Dependencies: []string{"2000000000_second"},
		Up:           func(db *pebble.DB) error { return nil },
		Down:         func(db *pebble.DB) error { return nil },
	}

	// Migration 4: Latest timestamp, depends on migration 1
	m4 := &Migration{
		ID:           "3000000000_fourth",
		Description:  "Fourth migration (depends on first)",
		Dependencies: []string{"1000000000_first"},
		Up:           func(db *pebble.DB) error { return nil },
		Down:         func(db *pebble.DB) error { return nil },
	}

	// Register in random order to test sorting
	if err := registry.Register(m3); err != nil {
		t.Fatalf("Failed to register m3: %v", err)
	}
	if err := registry.Register(m1); err != nil {
		t.Fatalf("Failed to register m1: %v", err)
	}
	if err := registry.Register(m4); err != nil {
		t.Fatalf("Failed to register m4: %v", err)
	}
	if err := registry.Register(m2); err != nil {
		t.Fatalf("Failed to register m2: %v", err)
	}

	// Get pending migrations (none applied)
	appliedMigrations := make(map[string]bool)
	pending, err := registry.GetPendingMigrations(appliedMigrations)
	if err != nil {
		t.Fatalf("Failed to get pending migrations: %v", err)
	}

	// Expected order:
	// 1. m1 (1000000000) - earliest timestamp, no dependencies
	// 2. m2 (2000000000) - second earliest timestamp, no dependencies
	// 3. m3 (1500000000) - despite earlier timestamp, must run after m2 due to dependency
	// 4. m4 (3000000000) - latest timestamp, depends on m1 which is already run

	expectedOrder := []string{
		"1000000000_first",
		"2000000000_second",
		"1500000000_third", // Runs after m2 despite earlier timestamp
		"3000000000_fourth",
	}

	if len(pending) != len(expectedOrder) {
		t.Fatalf("Expected %d migrations, got %d", len(expectedOrder), len(pending))
	}

	for i, m := range pending {
		if m.ID != expectedOrder[i] {
			t.Errorf("Position %d: expected %s, got %s", i, expectedOrder[i], m.ID)
		}
	}

	// Test with some migrations already applied
	appliedMigrations["1000000000_first"] = true
	appliedMigrations["2000000000_second"] = true

	pending, err = registry.GetPendingMigrations(appliedMigrations)
	if err != nil {
		t.Fatalf("Failed to get pending migrations with some applied: %v", err)
	}

	// Expected order with m1 and m2 applied:
	// 1. m3 (1500000000) - dependencies satisfied
	// 2. m4 (3000000000) - dependencies satisfied

	expectedOrder2 := []string{
		"1500000000_third",
		"3000000000_fourth",
	}

	if len(pending) != len(expectedOrder2) {
		t.Fatalf("Expected %d migrations, got %d", len(expectedOrder2), len(pending))
	}

	for i, m := range pending {
		if m.ID != expectedOrder2[i] {
			t.Errorf("Position %d: expected %s, got %s", i, expectedOrder2[i], m.ID)
		}
	}
}

func TestMigrationOrderingComplexDependencies(t *testing.T) {
	// Test more complex dependency chains
	registry := NewMigrationRegistry()

	// Create a diamond dependency pattern:
	//     m1
	//    /  \
	//   m2  m3
	//    \  /
	//     m4

	m1 := &Migration{
		ID:           "1000000000_base",
		Dependencies: []string{},
		Up:           func(db *pebble.DB) error { return nil },
		Down:         func(db *pebble.DB) error { return nil },
	}

	m2 := &Migration{
		ID:           "2000000000_left",
		Dependencies: []string{"1000000000_base"},
		Up:           func(db *pebble.DB) error { return nil },
		Down:         func(db *pebble.DB) error { return nil },
	}

	m3 := &Migration{
		ID:           "1500000000_right", // Earlier timestamp than m2
		Dependencies: []string{"1000000000_base"},
		Up:           func(db *pebble.DB) error { return nil },
		Down:         func(db *pebble.DB) error { return nil },
	}

	m4 := &Migration{
		ID:           "3000000000_merge",
		Dependencies: []string{"2000000000_left", "1500000000_right"},
		Up:           func(db *pebble.DB) error { return nil },
		Down:         func(db *pebble.DB) error { return nil },
	}

	// Register all
	registry.Register(m1)
	registry.Register(m2)
	registry.Register(m3)
	registry.Register(m4)

	// Get pending migrations
	appliedMigrations := make(map[string]bool)
	pending, err := registry.GetPendingMigrations(appliedMigrations)
	if err != nil {
		t.Fatalf("Failed to get pending migrations: %v", err)
	}

	// Expected order:
	// 1. m1 (base)
	// 2. m3 (right) - has earlier timestamp than m2
	// 3. m2 (left)
	// 4. m4 (merge) - can only run after both m2 and m3

	expectedOrder := []string{
		"1000000000_base",
		"1500000000_right", // Runs before m2 due to earlier timestamp
		"2000000000_left",
		"3000000000_merge",
	}

	if len(pending) != len(expectedOrder) {
		t.Fatalf("Expected %d migrations, got %d", len(expectedOrder), len(pending))
	}

	for i, m := range pending {
		if m.ID != expectedOrder[i] {
			t.Errorf("Position %d: expected %s, got %s", i, expectedOrder[i], m.ID)
		}
	}
}
