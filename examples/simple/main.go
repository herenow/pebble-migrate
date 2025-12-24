// Example: Simple migration usage
package main

import (
	"fmt"
	"log"
	"os"

	"github.com/cockroachdb/pebble"
	migrate "github.com/herenow/pebble-migrate"

	// Import migrations to register them via init()
	_ "github.com/herenow/pebble-migrate/examples/simple/migrations"
)

func main() {
	// Create a temporary database for demonstration
	dbPath := "./example_data"
	defer os.RemoveAll(dbPath)

	// Open database
	db, err := pebble.Open(dbPath, &pebble.Options{})
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	// Create migration services
	schemaManager := migrate.NewSchemaManager(db)
	registry := migrate.GlobalRegistry
	planner := migrate.NewMigrationPlanner(registry, schemaManager)

	// Check current status
	currentSchema, err := schemaManager.GetSchemaVersion()
	if err != nil {
		log.Fatalf("Failed to get schema version: %v", err)
	}
	fmt.Printf("Current version: %d\n", currentSchema.CurrentVersion)

	// Plan upgrade
	plan, err := planner.PlanUpgrade()
	if err != nil {
		log.Fatalf("Failed to plan upgrade: %v", err)
	}

	if len(plan.Migrations) == 0 {
		fmt.Println("No pending migrations")
		return
	}

	fmt.Printf("Found %d pending migrations\n", len(plan.Migrations))
	for _, m := range plan.Migrations {
		fmt.Printf("  - %s: %s\n", m.ID, m.Description)
	}

	// Execute migrations
	engine := migrate.NewMigrationEngineWithBackup(db, schemaManager, registry, dbPath)
	engine.SetBackupEnabled(false) // Disable backups for this example

	err = engine.ExecutePlan(plan, func(msg string) {
		fmt.Printf("[MIGRATION] %s\n", msg)
	})
	if err != nil {
		log.Fatalf("Migration failed: %v", err)
	}

	// Verify
	finalSchema, _ := schemaManager.GetSchemaVersion()
	fmt.Printf("Migration complete. Now at version: %d\n", finalSchema.CurrentVersion)
}
