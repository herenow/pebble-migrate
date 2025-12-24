// Example migration file
package migrations

import (
	"fmt"

	"github.com/cockroachdb/pebble"
	migrate "github.com/herenow/pebble-migrate"
)

func init() {
	migrate.Register(&migrate.Migration{
		ID:          "1700000000_example_migration",
		Description: "Add example index",
		Up:          exampleUp,
		Down:        exampleDown,
		Validate:    exampleValidate,
		Rerunnable:  true,
	})
}

func exampleUp(db *pebble.DB) error {
	// Example: Create an index entry
	indexKey := []byte("index:example:marker")
	indexValue := []byte("migration_applied")

	if err := db.Set(indexKey, indexValue, pebble.Sync); err != nil {
		return fmt.Errorf("failed to create index: %w", err)
	}

	fmt.Println("  Created example index")
	return nil
}

func exampleDown(db *pebble.DB) error {
	// Rollback: Remove the index entry
	indexKey := []byte("index:example:marker")

	if err := db.Delete(indexKey, pebble.Sync); err != nil {
		return fmt.Errorf("failed to delete index: %w", err)
	}

	fmt.Println("  Removed example index")
	return nil
}

func exampleValidate(db *pebble.DB) error {
	// Validate the migration succeeded
	indexKey := []byte("index:example:marker")

	value, closer, err := db.Get(indexKey)
	if err != nil {
		return fmt.Errorf("index not found: %w", err)
	}
	defer closer.Close()

	if string(value) != "migration_applied" {
		return fmt.Errorf("unexpected index value: %s", value)
	}

	fmt.Println("  Validation passed")
	return nil
}
