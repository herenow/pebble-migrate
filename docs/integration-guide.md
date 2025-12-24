# Integration Guide

How to integrate pebble-migrate with your application.

## Application Startup Integration

### Using CheckAndRunStartupMigrations (Recommended)

The simplest way to integrate migrations:

```go
package main

import (
    "log"

    "github.com/cockroachdb/pebble"
    migrate "github.com/herenow/pebble-migrate"

    // Import migrations to register them
    _ "your-app/migrations"
)

func main() {
    dbPath := "./data"

    db, err := pebble.Open(dbPath, &pebble.Options{})
    if err != nil {
        log.Fatalf("Failed to open database: %v", err)
    }
    defer db.Close()

    // Run migrations at startup
    opts := migrate.DefaultStartupOptions()
    opts.RunMigrations = true
    opts.CLIName = "your-app-migrate" // Customize error message CLI name

    if err := migrate.CheckAndRunStartupMigrations(db, dbPath, opts); err != nil {
        log.Fatalf("Migration failed: %v", err)
    }

    // Continue with application startup
    runApplication(db)
}
```

### Startup Options

```go
type StartupOptions struct {
    // RunMigrations enables migration execution during startup
    // If false, will fail if migrations are needed
    RunMigrations bool

    // Logger for migration progress (optional)
    Logger Logger

    // BackupEnabled controls whether backups are created
    // Default: false (backups are CPU intensive)
    BackupEnabled bool

    // CheckDiskSpace enables disk space validation
    // Default: true
    CheckDiskSpace bool

    // DatabaseSizeMultiplier for space calculation
    // Required free space = database size * multiplier
    // Default: 2.0
    DatabaseSizeMultiplier float64

    // CLIName shown in error messages
    // Default: "pebble-migrate"
    CLIName string
}
```

### Custom Logger Integration

```go
// Implement the Logger interface
type AppLogger struct {
    logger *slog.Logger
}

func (l *AppLogger) Printf(format string, args ...interface{}) {
    l.logger.Info(fmt.Sprintf(format, args...))
}

func (l *AppLogger) Debugf(format string, args ...interface{}) {
    l.logger.Debug(fmt.Sprintf(format, args...))
}

func (l *AppLogger) Errorf(format string, args ...interface{}) {
    l.logger.Error(fmt.Sprintf(format, args...))
}

// Use it
opts := migrate.DefaultStartupOptions()
opts.Logger = &AppLogger{logger: slog.Default()}
opts.RunMigrations = true
```

## Pre-Startup Migration Check

For more control, check migrations before starting:

```go
func checkMigrations(dbPath string) error {
    db, err := pebble.Open(dbPath, &pebble.Options{ReadOnly: true})
    if err != nil {
        return fmt.Errorf("failed to open database: %w", err)
    }
    defer db.Close()

    schemaManager := migrate.NewSchemaManager(db)
    currentSchema, err := schemaManager.GetSchemaVersion()
    if err != nil {
        return fmt.Errorf("failed to get schema version: %w", err)
    }

    // Check for pending migrations
    registry := migrate.GlobalRegistry
    planner := migrate.NewMigrationPlanner(registry, schemaManager)

    plan, err := planner.PlanUpgrade()
    if err != nil {
        return fmt.Errorf("failed to create plan: %w", err)
    }

    if len(plan.Migrations) > 0 {
        return fmt.Errorf("%d pending migrations - run 'your-app-migrate up'",
            len(plan.Migrations))
    }

    if currentSchema.Status != migrate.StatusClean {
        return fmt.Errorf("database in %s state", currentSchema.Status)
    }

    return nil
}
```

## Docker Integration

### Dockerfile

```dockerfile
FROM golang:1.21-alpine AS builder

WORKDIR /app
COPY . .
RUN go build -o app ./cmd/app
RUN go build -o app-migrate ./cmd/migrate

FROM alpine:latest
WORKDIR /app

COPY --from=builder /app/app /app/
COPY --from=builder /app/app-migrate /app/
COPY docker-entrypoint.sh /app/

RUN chmod +x /app/docker-entrypoint.sh
RUN mkdir -p /data

VOLUME ["/data"]
ENTRYPOINT ["/app/docker-entrypoint.sh"]
CMD ["./app"]
```

### Entrypoint Script

```bash
#!/bin/sh
# docker-entrypoint.sh

set -e

DB_PATH=${DATABASE_PATH:-/data}
AUTO_MIGRATE=${AUTO_MIGRATE:-false}

echo "Checking migration status..."
./app-migrate status --database "$DB_PATH"

if [ "$AUTO_MIGRATE" = "true" ]; then
    echo "Running migrations..."
    ./app-migrate up --database "$DB_PATH"
fi

echo "Starting application..."
exec "$@"
```

### Docker Compose

```yaml
version: '3.8'

services:
  app:
    build: .
    environment:
      DATABASE_PATH: "/data"
      AUTO_MIGRATE: "true"
    volumes:
      - app_data:/data
    healthcheck:
      test: ["CMD", "./app-migrate", "status", "-d", "/data"]
      interval: 30s
      timeout: 10s
      retries: 3

volumes:
  app_data:
```

## Kubernetes Deployment

### Init Container for Migrations

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: myapp
spec:
  template:
    spec:
      initContainers:
      - name: migrate
        image: myapp:latest
        command: ["./app-migrate", "up", "--database", "/data"]
        volumeMounts:
        - name: data
          mountPath: /data
      containers:
      - name: app
        image: myapp:latest
        volumeMounts:
        - name: data
          mountPath: /data
      volumes:
      - name: data
        persistentVolumeClaim:
          claimName: myapp-data
```

## CI/CD Integration

### GitHub Actions

```yaml
name: Test Migrations

on: [push, pull_request]

jobs:
  test-migrations:
    runs-on: ubuntu-latest
    steps:
    - uses: actions/checkout@v3

    - uses: actions/setup-go@v4
      with:
        go-version: '1.21'

    - name: Build migration tool
      run: go build -o app-migrate ./cmd/migrate

    - name: Test migrations
      run: |
        mkdir -p test_db
        ./app-migrate up --database test_db --dry-run
        ./app-migrate up --database test_db
        ./app-migrate validate --database test_db
        ./app-migrate down 0 --database test_db --dry-run
```

## Production Deployment Script

```bash
#!/bin/bash
set -e

DB_PATH="/var/lib/myapp/data"

echo "Creating pre-deployment backup..."
./app-migrate backup create "Pre-deployment $(date)" --database "$DB_PATH"

echo "Stopping application..."
systemctl stop myapp

echo "Running migrations..."
./app-migrate up --database "$DB_PATH" --verbose

echo "Validating database..."
./app-migrate validate --database "$DB_PATH"

echo "Starting application..."
systemctl start myapp

echo "Health check..."
sleep 10
if ! curl -f http://localhost:8080/health; then
    echo "Health check failed! Consider rollback."
    exit 1
fi

echo "Deployment completed successfully!"
```

## Health Check Integration

```go
func healthCheckHandler(db *pebble.DB) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        schemaManager := migrate.NewSchemaManager(db)
        schema, err := schemaManager.GetSchemaVersion()
        if err != nil {
            http.Error(w, "Migration status unavailable",
                http.StatusServiceUnavailable)
            return
        }

        if schema.Status != migrate.StatusClean {
            http.Error(w, fmt.Sprintf("Database in %s state", schema.Status),
                http.StatusServiceUnavailable)
            return
        }

        json.NewEncoder(w).Encode(map[string]interface{}{
            "status":           "healthy",
            "database_version": schema.CurrentVersion,
            "last_migration":   schema.LastMigrationAt,
        })
    }
}
```
