# pebble-migrate

A comprehensive database migration system for [Pebble](https://github.com/cockroachdb/pebble) with schema versioning, data validation, and CLI management capabilities.

## Features

- **Schema Versioning**: Track database schema versions using Unix timestamps
- **Migration Management**: Execute forward and rollback migrations safely
- **Dependency Support**: Define migration dependencies for complex upgrade paths
- **Data Validation**: Optional post-migration validation functions
- **Automatic Backups**: Create compressed backups before migrations
- **Recovery Support**: Handle interrupted migrations with rerunnable migrations
- **CLI Interface**: Full-featured command-line tool
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

## Quick Start

### 1. Create a Migration

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
    return db.Set([]byte("index:users:email"), []byte("enabled"), pebble.Sync)
}

func removeIndexes(db *pebble.DB) error {
    return db.Delete([]byte("index:users:email"), pebble.Sync)
}
```

### 2. Run Migrations

Using CLI:
```bash
# Check status
pebble-migrate status --database ./data

# Apply migrations
pebble-migrate up --database ./data
```

Or programmatically:
```go
package main

import (
    "log"

    "github.com/cockroachdb/pebble"
    migrate "github.com/herenow/pebble-migrate"
    _ "your-app/migrations"
)

func main() {
    db, _ := pebble.Open("./data", &pebble.Options{})
    defer db.Close()

    opts := migrate.DefaultStartupOptions()
    opts.RunMigrations = true

    if err := migrate.CheckAndRunStartupMigrations(db, "./data", opts); err != nil {
        log.Fatal(err)
    }
}
```

## CLI Commands

| Command | Description |
|---------|-------------|
| `status` | Show current migration status |
| `up [version]` | Apply pending migrations |
| `down <version>` | Rollback to a specific version |
| `rerun <id>` | Rerun a specific migration |
| `validate` | Validate database integrity |
| `history` | Show migration history |
| `backup create` | Create a manual backup |
| `backup list` | List available backups |
| `backup restore` | Restore from backup |
| `force-clean` | Force database to clean state |

See [CLI Reference](docs/cli-reference.md) for complete documentation.

## Migration ID Format

Migration IDs follow the format: `<unix_timestamp>_<description>`

Generate a timestamp:
```bash
date +%s  # e.g., 1700000000
```

## Dependencies

Migrations can declare dependencies:

```go
migrate.Register(&migrate.Migration{
    ID:           "1700100000_add_user_indexes",
    Description:  "Add user indexes",
    Dependencies: []string{"1700000000_create_users"},
    Up:           addUserIndexes,
    Down:         removeUserIndexes,
})
```

Migrations with dependencies run after their dependencies, regardless of timestamp.

## Documentation

- [Getting Started](docs/getting-started.md)
- [CLI Reference](docs/cli-reference.md)
- [Writing Migrations](docs/writing-migrations.md)
- [Integration Guide](docs/integration-guide.md)
- [Recovery Guide](docs/recovery-guide.md)

## Examples

See the [examples](examples/) directory for complete working examples:
- [Simple migration](examples/simple/) - Basic migration usage
- [Startup integration](examples/with-startup/) - Application startup integration

## Contributing

Contributions are welcome! Please see [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

## License

MIT License - see [LICENSE](LICENSE) for details.
