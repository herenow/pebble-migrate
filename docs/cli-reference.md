# CLI Reference

Complete reference for the pebble-migrate command-line interface.

## Global Flags

These flags are available for all commands:

| Flag | Short | Description |
|------|-------|-------------|
| `--database` | `-d` | Path to Pebble database (required) |
| `--verbose` | `-v` | Enable verbose output |
| `--dry-run` | `-n` | Show what would be done without executing |

## Commands

### status

Show current migration status.

```bash
pebble-migrate status --database /path/to/db
```

Displays:
- Current schema version (Unix timestamp)
- Migration status (clean, dirty, migrating)
- Applied migrations with timestamps
- Pending migrations
- Migration history and statistics

### up

Apply pending migrations.

```bash
# Apply all pending migrations
pebble-migrate up --database /path/to/db

# Migrate to specific version (Unix timestamp)
pebble-migrate up 1754917200 --database /path/to/db

# Dry run
pebble-migrate up --database /path/to/db --dry-run

# Skip backup
pebble-migrate up --database /path/to/db --no-backup
```

**Flags:**
- `--no-backup`: Skip automatic backup creation

### down

Rollback migrations to a specific version.

```bash
# Rollback to specific version (Unix timestamp)
pebble-migrate down 1754917200 --database /path/to/db

# Rollback all migrations
pebble-migrate down 0 --database /path/to/db

# Dry run
pebble-migrate down 1754917200 --database /path/to/db --dry-run
```

**Flags:**
- `--no-backup`: Skip automatic backup creation

### rerun

Rerun a specific migration (rollback then apply).

```bash
# Rerun a specific migration
pebble-migrate rerun 1700000000_add_indexes --database /path/to/db

# Dry run
pebble-migrate rerun 1700000000_add_indexes --database /path/to/db --dry-run
```

**Flags:**
- `--no-backup`: Skip automatic backup creation

### validate

Validate database integrity and migration state.

```bash
# Run validation
pebble-migrate validate --database /path/to/db

# Verbose validation
pebble-migrate validate --database /path/to/db --verbose
```

Validates:
- Schema version consistency
- Migration history integrity
- Migration registry configuration

### history

Show detailed migration history.

```bash
pebble-migrate history --database /path/to/db
```

Displays:
- All applied migrations with timestamps
- Rollback history
- Failed migrations with error messages
- Duration of each migration

### backup

Manage database backups.

#### backup create

Create a manual backup.

```bash
pebble-migrate backup create "Before major update" --database /path/to/db
pebble-migrate backup create --database /path/to/db
```

#### backup list

List available backups.

```bash
pebble-migrate backup list --database /path/to/db
```

#### backup restore

Restore from a backup.

```bash
pebble-migrate backup restore /path/to/backup --database /path/to/db
pebble-migrate backup restore /path/to/backup --database /path/to/db --force
```

**Flags:**
- `--force`: Skip confirmation prompt

#### backup cleanup

Clean up old backups.

```bash
pebble-migrate backup cleanup --older-than 30d --database /path/to/db
pebble-migrate backup cleanup --older-than 7d --database /path/to/db
```

**Flags:**
- `--older-than`: Remove backups older than this duration (e.g., 7d, 30d, 24h)

### force-clean

Force the database to clean state.

```bash
pebble-migrate force-clean --database /path/to/db
```

**WARNING**: This is a dangerous operation that bypasses safety checks. Only use when:
- You understand the current state of your database
- You have backups of your data
- Normal migration operations are failing due to state issues

### create

Generate a new migration file template.

```bash
pebble-migrate create add_user_indexes
```

**Note**: This command provides guidance for manual migration file creation.

## Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | Error occurred |

## Examples

### Complete Migration Workflow

```bash
# 1. Check current status
pebble-migrate status -d ./data

# 2. Dry run to preview changes
pebble-migrate up -d ./data --dry-run

# 3. Create backup manually (optional, migrations auto-backup)
pebble-migrate backup create "Pre-migration backup" -d ./data

# 4. Apply migrations
pebble-migrate up -d ./data -v

# 5. Validate database
pebble-migrate validate -d ./data

# 6. Check history
pebble-migrate history -d ./data
```

### Rollback Workflow

```bash
# 1. Check history for version to rollback to
pebble-migrate history -d ./data

# 2. Preview rollback
pebble-migrate down 1700000000 -d ./data --dry-run

# 3. Execute rollback
pebble-migrate down 1700000000 -d ./data

# 4. Validate
pebble-migrate validate -d ./data
```
