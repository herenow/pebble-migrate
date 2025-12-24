# Getting Started with pebble-migrate

A comprehensive database migration system for Pebble with schema versioning, data validation, and CLI management capabilities.

## Overview

pebble-migrate provides:
- **Schema Versioning**: Track and manage database schema versions using Unix timestamps
- **Migration Management**: Execute forward and rollback migrations safely
- **Data Validation**: Comprehensive data integrity validation
- **CLI Interface**: Easy-to-use command-line tools
- **Backup System**: Automatic backups before migrations
- **Application Integration**: Built-in startup migration support

## Installation

### As a Library

```bash
go get github.com/herenow/pebble-migrate
```

### CLI Tool

```bash
go install github.com/herenow/pebble-migrate/cmd/pebble-migrate@latest
```

Or build from source:
```bash
git clone https://github.com/herenow/pebble-migrate.git
cd pebble-migrate
go build -o pebble-migrate ./cmd/pebble-migrate
```

## Quick Start

### 1. Create Your First Migration

Create a migrations directory and add a migration file:

```go
// migrations/1700000000_add_indexes.go
package migrations

import (
    "github.com/cockroachdb/pebble"
    migrate "github.com/herenow/pebble-migrate"
)

func init() {
    migrate.Register(&migrate.Migration{
        ID:          "1700000000_add_indexes",
        Description: "Add initial indexes",
        Up:          addIndexes,
        Down:        removeIndexes,
        Rerunnable:  true,
    })
}

func addIndexes(db *pebble.DB) error {
    // Your migration logic here
    return nil
}

func removeIndexes(db *pebble.DB) error {
    // Your rollback logic here
    return nil
}
```

### 2. Import Migrations in Your Application

```go
package main

import (
    _ "your-app/migrations" // Import to register migrations
)
```

### 3. Check Status

```bash
./pebble-migrate status --database /path/to/pebble/db
```

### 4. Run Migrations

```bash
# Apply all pending migrations
./pebble-migrate up --database /path/to/pebble/db

# Dry run to see what would happen
./pebble-migrate up --database /path/to/pebble/db --dry-run
```

## Application Integration

For automatic migration handling during application startup:

```go
package main

import (
    "log"

    "github.com/cockroachdb/pebble"
    migrate "github.com/herenow/pebble-migrate"
    _ "your-app/migrations"
)

func main() {
    dbPath := "./data"

    // Open database
    db, err := pebble.Open(dbPath, &pebble.Options{})
    if err != nil {
        log.Fatalf("Failed to open database: %v", err)
    }
    defer db.Close()

    // Check and run migrations
    opts := migrate.DefaultStartupOptions()
    opts.RunMigrations = true // Enable automatic migrations
    opts.CLIName = "my-app"   // Customize CLI name in error messages

    if err := migrate.CheckAndRunStartupMigrations(db, dbPath, opts); err != nil {
        log.Fatalf("Migration failed: %v", err)
    }

    // Continue with application startup
    log.Println("Application started successfully")
}
```

## Next Steps

- [CLI Reference](cli-reference.md) - Full command documentation
- [Writing Migrations](writing-migrations.md) - How to write effective migrations
- [Integration Guide](integration-guide.md) - Application integration patterns
- [Recovery Guide](recovery-guide.md) - Troubleshooting and recovery procedures
