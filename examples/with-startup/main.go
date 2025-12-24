// Example: Using CheckAndRunStartupMigrations for application startup
package main

import (
	"log"
	"os"

	"github.com/cockroachdb/pebble"
	migrate "github.com/herenow/pebble-migrate"

	// Import migrations to register them
	_ "github.com/herenow/pebble-migrate/examples/simple/migrations"
)

func main() {
	dbPath := "./startup_example_data"
	defer os.RemoveAll(dbPath)

	// Open database
	db, err := pebble.Open(dbPath, &pebble.Options{})
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	// Configure startup options
	opts := migrate.DefaultStartupOptions()
	opts.RunMigrations = true       // Enable automatic migrations
	opts.BackupEnabled = false      // Disable backups for this example
	opts.CLIName = "myapp-migrate"  // Customize CLI name in error messages
	opts.Logger = &AppLogger{}      // Use custom logger

	// Check and run migrations at startup
	if err := migrate.CheckAndRunStartupMigrations(db, dbPath, opts); err != nil {
		log.Fatalf("Startup migration failed: %v", err)
	}

	log.Println("Application started successfully!")
	log.Println("Database is up to date and ready for use")

	// Your application logic continues here...
	runApplication(db)
}

// AppLogger implements the migrate.Logger interface
type AppLogger struct{}

func (l *AppLogger) Printf(format string, args ...interface{}) {
	log.Printf("[INFO] "+format, args...)
}

func (l *AppLogger) Debugf(format string, args ...interface{}) {
	log.Printf("[DEBUG] "+format, args...)
}

func (l *AppLogger) Errorf(format string, args ...interface{}) {
	log.Printf("[ERROR] "+format, args...)
}

func runApplication(db *pebble.DB) {
	// Simulate application running
	log.Println("Application is running...")

	// Example: Read data
	value, closer, err := db.Get([]byte("index:example:marker"))
	if err != nil {
		log.Printf("No marker found (expected on first run): %v", err)
		return
	}
	defer closer.Close()

	log.Printf("Found marker value: %s", value)
}
