# Writing Migrations

Guide for creating effective database migrations with pebble-migrate.

## Migration Structure

Each migration is a Go file that registers itself via `init()`:

```go
package migrations

import (
    "github.com/cockroachdb/pebble"
    migrate "github.com/herenow/pebble-migrate"
)

func init() {
    migrate.Register(&migrate.Migration{
        ID:           "1700000000_example_migration",
        Description:  "Add new indexes",
        Dependencies: []string{},              // Optional dependencies
        Up:           upFunction,
        Down:         downFunction,
        Validate:     validateFunction,        // Optional
        Rerunnable:   true,                    // Optional: if safe to rerun
    })
}

func upFunction(db *pebble.DB) error {
    // Migration logic here
    return nil
}

func downFunction(db *pebble.DB) error {
    // Rollback logic here
    return nil
}

func validateFunction(db *pebble.DB) error {
    // Validation logic here
    return nil
}
```

## Migration ID Format

Migration IDs must follow this format:
```
<unix_timestamp>_<description>
```

### Generating a Timestamp

```bash
# Get current Unix timestamp
date +%s
# Example output: 1700000000

# Create migration file
touch migrations/1700000000_add_user_indexes.go
```

### Valid Examples
- `1700000000_add_indexes`
- `1700000001_migrate_data_format`
- `1700000002_cleanup_legacy_keys`

### Invalid Examples
- `001_add_indexes` (non-Unix timestamp)
- `1700000000` (missing description)
- `add_indexes` (missing timestamp)

## Migration Fields

### Required Fields

| Field | Type | Description |
|-------|------|-------------|
| `ID` | `string` | Unique identifier in `timestamp_description` format |
| `Description` | `string` | Human-readable description |
| `Up` | `func(*pebble.DB) error` | Forward migration function |
| `Down` | `func(*pebble.DB) error` | Rollback function |

### Optional Fields

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `Dependencies` | `[]string` | `nil` | IDs of migrations that must run first |
| `Validate` | `func(*pebble.DB) error` | `nil` | Post-migration validation |
| `Rerunnable` | `bool` | `false` | If true, safe to rerun after interruption |

## Migration Ordering

Migrations are executed based on:

1. **Dependencies**: Migrations with dependencies run after their dependencies
2. **Timestamp**: When no dependency exists, migrations run in chronological order

### Example

```go
// Migration A: timestamp 1700000000, no dependencies
// Migration B: timestamp 1700100000, no dependencies
// Migration C: timestamp 1700050000, depends on B
// Migration D: timestamp 1700200000, depends on A

// Execution order: A → B → C → D
```

### Diamond Dependencies

```go
// Diamond pattern:
//     A
//    / \
//   B   C
//    \ /
//     D

// A: 1700000000, no deps
// B: 1700100000, depends on A
// C: 1700050000, depends on A (earlier than B)
// D: 1700200000, depends on B and C

// Execution order: A → C → B → D (C before B due to earlier timestamp)
```

## Best Practices

### 1. Make Migrations Idempotent When Possible

```go
func upMigration(db *pebble.DB) error {
    // Check if already migrated
    _, closer, err := db.Get([]byte("_migration_marker_xyz"))
    if err == nil {
        closer.Close()
        return nil // Already migrated
    }

    // Do migration work...

    // Mark as migrated
    return db.Set([]byte("_migration_marker_xyz"), []byte("done"), pebble.Sync)
}
```

### 2. Use Batch Operations for Efficiency

```go
func upMigration(db *pebble.DB) error {
    batch := db.NewBatch()
    defer batch.Close()

    // Multiple operations in one batch
    batch.Set([]byte("key1"), []byte("value1"), nil)
    batch.Set([]byte("key2"), []byte("value2"), nil)
    batch.Delete([]byte("old_key"), nil)

    return batch.Commit(pebble.Sync)
}
```

### 3. Handle Large Data Sets with Iteration

```go
func upMigration(db *pebble.DB) error {
    iter, _ := db.NewIter(&pebble.IterOptions{
        LowerBound: []byte("prefix:"),
        UpperBound: []byte("prefix:\xff"),
    })
    defer iter.Close()

    batch := db.NewBatch()
    count := 0

    for iter.First(); iter.Valid(); iter.Next() {
        // Process each key
        newValue := processValue(iter.Value())
        batch.Set(iter.Key(), newValue, nil)

        count++
        if count >= 1000 {
            if err := batch.Commit(pebble.Sync); err != nil {
                return err
            }
            batch = db.NewBatch()
            count = 0
        }
    }

    return batch.Commit(pebble.Sync)
}
```

### 4. Always Implement Down Migration

```go
func downMigration(db *pebble.DB) error {
    // Reverse the up migration
    // This is critical for rollback support
    return db.Delete([]byte("new_key"), pebble.Sync)
}
```

### 5. Add Validation When Critical

```go
func validateMigration(db *pebble.DB) error {
    // Verify migration succeeded
    value, closer, err := db.Get([]byte("expected_key"))
    if err != nil {
        return fmt.Errorf("expected key not found: %w", err)
    }
    defer closer.Close()

    if string(value) != "expected_value" {
        return fmt.Errorf("unexpected value: %s", value)
    }

    return nil
}
```

### 6. Mark Resumable Migrations

```go
migrate.Register(&migrate.Migration{
    ID:          "1700000000_format_conversion",
    Description: "Convert data format",
    Up:          convertFormat,
    Down:        revertFormat,
    Rerunnable:  true, // Safe to rerun if interrupted
})
```

## Testing Migrations

### Unit Tests

```go
func TestMigration(t *testing.T) {
    // Create temp database
    dir := t.TempDir()
    db, err := pebble.Open(dir, &pebble.Options{})
    require.NoError(t, err)
    defer db.Close()

    // Setup initial state
    err = db.Set([]byte("old_key"), []byte("old_value"), pebble.Sync)
    require.NoError(t, err)

    // Run up migration
    err = upMigration(db)
    require.NoError(t, err)

    // Verify result
    value, closer, err := db.Get([]byte("new_key"))
    require.NoError(t, err)
    closer.Close()
    assert.Equal(t, "new_value", string(value))

    // Test down migration
    err = downMigration(db)
    require.NoError(t, err)

    // Verify rollback
    _, _, err = db.Get([]byte("new_key"))
    assert.Equal(t, pebble.ErrNotFound, err)
}
```

### Integration Tests

```go
func TestMigrationFlow(t *testing.T) {
    dir := t.TempDir()
    db, _ := pebble.Open(dir, &pebble.Options{})
    defer db.Close()

    registry := migrate.NewMigrationRegistry()
    schemaManager := migrate.NewSchemaManager(db)
    engine := migrate.NewMigrationEngineWithBackup(db, schemaManager, registry, dir)

    // Register test migration
    registry.Register(&migrate.Migration{
        ID:          "1700000000_test",
        Description: "Test migration",
        Up:          func(db *pebble.DB) error { return nil },
        Down:        func(db *pebble.DB) error { return nil },
    })

    // Run migration
    planner := migrate.NewMigrationPlanner(registry, schemaManager)
    plan, _ := planner.PlanUpgrade()

    err := engine.ExecutePlan(plan, func(msg string) {
        t.Log(msg)
    })
    require.NoError(t, err)

    // Verify
    version, _ := schemaManager.GetSchemaVersion()
    assert.Equal(t, int64(1700000000), version.CurrentVersion)
}
```

## Common Patterns

### Data Format Migration

```go
func migrateDataFormat(db *pebble.DB) error {
    iter, _ := db.NewIter(&pebble.IterOptions{
        LowerBound: []byte("data:"),
        UpperBound: []byte("data:\xff"),
    })
    defer iter.Close()

    for iter.First(); iter.Valid(); iter.Next() {
        oldData := iter.Value()

        // Check if already new format
        if isNewFormat(oldData) {
            continue
        }

        // Convert to new format
        newData, err := convertFormat(oldData)
        if err != nil {
            return err
        }

        // Write back
        if err := db.Set(iter.Key(), newData, pebble.Sync); err != nil {
            return err
        }
    }

    return nil
}
```

### Adding Indexes

```go
func addIndex(db *pebble.DB) error {
    iter, _ := db.NewIter(&pebble.IterOptions{
        LowerBound: []byte("entity:"),
        UpperBound: []byte("entity:\xff"),
    })
    defer iter.Close()

    batch := db.NewBatch()

    for iter.First(); iter.Valid(); iter.Next() {
        entity := parseEntity(iter.Value())
        indexKey := []byte(fmt.Sprintf("index:%s:%s", entity.Type, entity.ID))
        batch.Set(indexKey, iter.Key(), nil)
    }

    return batch.Commit(pebble.Sync)
}
```

### Cleanup/Deletion

```go
func cleanupOldData(db *pebble.DB) error {
    iter, _ := db.NewIter(&pebble.IterOptions{
        LowerBound: []byte("legacy:"),
        UpperBound: []byte("legacy:\xff"),
    })
    defer iter.Close()

    batch := db.NewBatch()

    for iter.First(); iter.Valid(); iter.Next() {
        batch.Delete(slices.Clone(iter.Key()), nil)
    }

    return batch.Commit(pebble.Sync)
}
```
