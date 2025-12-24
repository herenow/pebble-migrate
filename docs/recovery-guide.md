# Migration Recovery Guide

How to recover from interrupted or failed migrations.

## Migration States

| State | Description |
|-------|-------------|
| `clean` | All migrations applied successfully |
| `migrating` | Migration currently in progress |
| `dirty` | Migration failed, needs intervention |
| `rollback` | Rollback in progress |

## Recovery Scenarios

### 1. Migration Interrupted (Stuck in "migrating" state)

If the program exits during migration (e.g., Ctrl+C, crash, OOM), the database will be stuck in "migrating" state.

#### Automatic Recovery (Recommended)

If the migration is marked as `Rerunnable: true`, the startup check will automatically recover:

```go
// Startup handles rerunnable migrations automatically
opts := migrate.DefaultStartupOptions()
opts.RunMigrations = true
err := migrate.CheckAndRunStartupMigrations(db, dbPath, opts)
```

#### Manual Recovery

```bash
# Check current state
pebble-migrate status --database /path/to/db

# Force clean state (if migration is idempotent)
pebble-migrate force-clean --database /path/to/db

# Re-run the migration
pebble-migrate up --database /path/to/db
```

#### Restore from Backup

```bash
# List available backups
pebble-migrate backup list --database /path/to/db

# Restore from backup
pebble-migrate backup restore /path/to/backup --database /path/to/db --force

# Re-attempt migration
pebble-migrate up --database /path/to/db
```

### 2. Migration Failed (Stuck in "dirty" state)

This happens when a migration encounters an error.

```bash
# Check what went wrong
pebble-migrate status --database /path/to/db --verbose

# Check migration history for error details
pebble-migrate history --database /path/to/db

# Option 1: Force clean and retry (if safe)
pebble-migrate force-clean --database /path/to/db
pebble-migrate up --database /path/to/db

# Option 2: Rollback to previous version
pebble-migrate down [previous_version] --database /path/to/db

# Option 3: Restore from backup
pebble-migrate backup restore /path/to/backup --database /path/to/db --force
```

### 3. Validation Failures After Migration

```bash
# Run detailed validation
pebble-migrate validate --database /path/to/db --verbose

# If validation fails, consider rollback
pebble-migrate down [previous_version] --database /path/to/db
```

## Best Practices

### Before Running Migrations

1. **Verify backup exists** (created automatically, but check):
   ```bash
   pebble-migrate backup list --database /path/to/db
   ```

2. **Check current state**:
   ```bash
   pebble-migrate status --database /path/to/db
   ```

3. **Run in dry-run mode first**:
   ```bash
   pebble-migrate up --database /path/to/db --dry-run
   ```

### During Migration

1. **Monitor progress**: Use `--verbose` flag
2. **Don't force-kill**: Use Ctrl+C once and let it clean up
3. **Watch resources**: Monitor memory and disk space

### After Migration

1. **Validate immediately**:
   ```bash
   pebble-migrate validate --database /path/to/db
   ```

2. **Test the application**: Start your app and verify functionality

3. **Keep backups**: Don't delete backups immediately

## Emergency Recovery

If all else fails:

1. **Stop all application processes**

2. **Create backup of corrupted state** (for investigation):
   ```bash
   cp -r /path/to/db /path/to/db.corrupted.backup
   ```

3. **Restore from last known good backup**:
   ```bash
   rm -rf /path/to/db
   pebble-migrate backup restore /path/to/good-backup /path/to/db --force
   ```

4. **Verify restoration**:
   ```bash
   pebble-migrate status --database /path/to/db
   pebble-migrate validate --database /path/to/db
   ```

## Command Quick Reference

| Command | Description |
|---------|-------------|
| `pebble-migrate status -d /path/to/db` | Check current state |
| `pebble-migrate force-clean -d /path/to/db` | Force state to clean |
| `pebble-migrate up -d /path/to/db` | Run pending migrations |
| `pebble-migrate down [version] -d /path/to/db` | Rollback to version |
| `pebble-migrate validate -d /path/to/db` | Validate database |
| `pebble-migrate backup list -d /path/to/db` | List backups |
| `pebble-migrate backup restore [path] -d /path/to/db --force` | Restore backup |

## Important Notes

1. **Rerunnable Migrations**: Migrations marked as `Rerunnable: true` can be safely re-executed after interruption. The migration should check for already-processed items and skip them.

2. **Backup Retention**: Keep backups from before major migrations for at least 30 days.

3. **Testing**: Always test migrations in a development environment first.

4. **Monitoring**: After recovery, monitor the application closely for any issues.
