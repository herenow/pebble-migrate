package migrate

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/cockroachdb/pebble"
)

// SchemaVersion represents the current schema state and applied migrations
type SchemaVersion struct {
	CurrentVersion    int64             `json:"current_version"`    // Unix timestamp of last applied migration (0 if none)
	AppliedMigrations map[string]bool   `json:"applied_migrations"` // Set of applied migration IDs
	MigrationHistory  []MigrationRecord `json:"migration_history"`  // Historical record of migrations
	LastMigrationAt   time.Time         `json:"last_migration_at"`
	Status            Status            `json:"status"`
}

// MigrationRecord tracks when and how a migration was applied
type MigrationRecord struct {
	ID          string    `json:"id"`          // Timestamp-based ID (e.g., "20250812_143022_description")
	Description string    `json:"description"`
	AppliedAt   time.Time `json:"applied_at"`
	Duration    string    `json:"duration"`
	Success     bool      `json:"success"`
	Error       string    `json:"error,omitempty"`
}

// Status represents the current migration state
type Status string

const (
	StatusClean     Status = "clean"     // All migrations applied successfully
	StatusMigrating Status = "migrating" // Migration in progress
	StatusDirty     Status = "dirty"     // Migration failed, needs manual intervention
	StatusRollback  Status = "rollback"  // Rollback in progress
)

// Migration represents a single database migration
type Migration struct {
	ID           string        // Unix timestamp ID (e.g., "1736700000_marketmeta_migration")
	Version      int64         // Unix timestamp parsed from ID (e.g., 1736700000)
	Dependencies []string      // IDs of migrations that must be applied before this one
	Description  string
	Up           MigrationFunc
	Down         MigrationFunc
	Validate     MigrationFunc
	Rerunnable   bool          // If true, migration can be safely rerun if interrupted
}

// MigrationFunc is the signature for migration functions
type MigrationFunc func(db *pebble.DB) error

// MigrationRegistry manages all available migrations
type MigrationRegistry struct {
	migrations map[string]*Migration
	ordered    []*Migration
}

// NewMigrationRegistry creates a new migration registry
func NewMigrationRegistry() *MigrationRegistry {
	return &MigrationRegistry{
		migrations: make(map[string]*Migration),
		ordered:    make([]*Migration, 0),
	}
}

// Register adds a migration to the registry
func (r *MigrationRegistry) Register(m *Migration) error {
	if _, exists := r.migrations[m.ID]; exists {
		return fmt.Errorf("migration with ID '%s' already registered", m.ID)
	}

	// Validate migration
	if m.ID == "" {
		return fmt.Errorf("migration ID cannot be empty")
	}
	if m.Up == nil {
		return fmt.Errorf("migration '%s' must have an Up function", m.ID)
	}
	if m.Down == nil {
		return fmt.Errorf("migration '%s' must have a Down function", m.ID)
	}

	// Parse and validate Unix timestamp from ID
	version, err := ParseMigrationVersion(m.ID)
	if err != nil {
		return fmt.Errorf("invalid migration ID format '%s': %w", m.ID, err)
	}
	m.Version = version

	r.migrations[m.ID] = m
	r.ordered = append(r.ordered, m)

	// Keep ordered by version (Unix timestamp)
	for i := len(r.ordered) - 1; i > 0; i-- {
		if r.ordered[i].Version < r.ordered[i-1].Version {
			r.ordered[i], r.ordered[i-1] = r.ordered[i-1], r.ordered[i]
		} else {
			break
		}
	}

	return nil
}

// GetMigration returns a migration by ID
func (r *MigrationRegistry) GetMigration(id string) (*Migration, bool) {
	m, exists := r.migrations[id]
	return m, exists
}

// GetMigrations returns all migrations ordered by version
func (r *MigrationRegistry) GetMigrations() []*Migration {
	return r.ordered
}

// GetPendingMigrations returns migrations that haven't been applied yet.
// Migrations are ordered by:
// 1. Dependencies (migrations run after their dependencies)
// 2. Unix timestamp (earlier migrations run first when no dependency relationship exists)
// This ensures a deterministic and chronological execution order.
func (r *MigrationRegistry) GetPendingMigrations(appliedMigrations map[string]bool) ([]*Migration, error) {
	var pending []*Migration

	// First collect all pending migrations
	for _, m := range r.ordered {
		// Skip if already applied
		if appliedMigrations[m.ID] {
			continue
		}
		pending = append(pending, m)
	}

	// Perform topological sort based on dependencies
	sorted, err := r.topologicalSort(pending, appliedMigrations)
	if err != nil {
		return nil, fmt.Errorf("failed to sort migrations by dependencies: %w", err)
	}

	return sorted, nil
}

// GetMigrationsInVersionRange returns migrations between two versions (inclusive)
func (r *MigrationRegistry) GetMigrationsInVersionRange(fromVersion, toVersion int64) []*Migration {
	var result []*Migration
	for _, m := range r.ordered {
		if m.Version >= fromVersion && m.Version <= toVersion {
			result = append(result, m)
		}
	}
	return result
}

// ParseMigrationVersion parses Unix timestamp version from migration ID
// Expected format: <unix_timestamp>_<description>
// Example: 1736700000_marketmeta_migration
func ParseMigrationVersion(migrationID string) (int64, error) {
	// Split on first underscore
	parts := strings.SplitN(migrationID, "_", 2)
	if len(parts) != 2 {
		return 0, fmt.Errorf("migration ID must follow format <timestamp>_<description>")
	}

	// Parse Unix timestamp
	version, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid timestamp in migration ID: %w", err)
	}

	// Validate it's a reasonable Unix timestamp (between year 2000 and 2100)
	if version < 946684800 || version > 4102444800 {
		return 0, fmt.Errorf("timestamp %d is outside valid range (2000-2100)", version)
	}

	return version, nil
}


// FormatVersionAsTime converts Unix timestamp to human-readable time
func FormatVersionAsTime(version int64) string {
	if version == 0 {
		return "(no migrations)"
	}
	return time.Unix(version, 0).UTC().Format("2006-01-02 15:04:05 UTC")
}

// topologicalSort performs a topological sort on migrations based on dependencies
func (r *MigrationRegistry) topologicalSort(migrations []*Migration, appliedMigrations map[string]bool) ([]*Migration, error) {
	if len(migrations) == 0 {
		return migrations, nil
	}

	// Build dependency graph
	graph := make(map[string][]string) // migration ID -> list of IDs that depend on it
	inDegree := make(map[string]int)   // migration ID -> count of unmet dependencies
	migrationMap := make(map[string]*Migration)

	// Initialize graph
	for _, m := range migrations {
		migrationMap[m.ID] = m
		inDegree[m.ID] = 0
		graph[m.ID] = []string{}
	}

	// Build edges and calculate in-degrees
	for _, m := range migrations {
		for _, depID := range m.Dependencies {
			// Only count dependency if it's not already applied
			if !appliedMigrations[depID] {
				// Check if dependency exists in our pending set
				if _, exists := migrationMap[depID]; !exists {
					// Check if dependency exists at all
					if _, exists := r.migrations[depID]; !exists {
						return nil, fmt.Errorf("migration %s depends on non-existent migration %s", m.ID, depID)
					}
					// Dependency exists but not in pending set - it must be applied already
					if !appliedMigrations[depID] {
						return nil, fmt.Errorf("migration %s depends on %s which is neither applied nor pending", m.ID, depID)
					}
				} else {
					// Dependency is in pending set
					graph[depID] = append(graph[depID], m.ID)
					inDegree[m.ID]++
				}
			}
		}
	}

	// Kahn's algorithm for topological sort with timestamp ordering
	var sorted []*Migration
	var ready []*Migration // Migrations ready to be processed (no dependencies)

	// Find all nodes with no dependencies
	for id, degree := range inDegree {
		if degree == 0 {
			ready = append(ready, migrationMap[id])
		}
	}

	// Process migrations, always picking the one with lowest timestamp from ready set
	for len(ready) > 0 {
		// Sort ready migrations by timestamp to maintain chronological order when possible
		for i := 0; i < len(ready)-1; i++ {
			for j := i + 1; j < len(ready); j++ {
				if ready[i].Version > ready[j].Version {
					ready[i], ready[j] = ready[j], ready[i]
				}
			}
		}

		// Take the migration with lowest timestamp
		current := ready[0]
		ready = ready[1:]

		// Add to sorted result
		sorted = append(sorted, current)

		// Reduce in-degree for dependent migrations
		for _, dependent := range graph[current.ID] {
			inDegree[dependent]--
			if inDegree[dependent] == 0 {
				ready = append(ready, migrationMap[dependent])
			}
		}
	}

	// Check for cycles
	if len(sorted) != len(migrations) {
		// Find migrations involved in cycle
		var cycleMigrations []string
		for id, degree := range inDegree {
			if degree > 0 {
				cycleMigrations = append(cycleMigrations, id)
			}
		}
		return nil, fmt.Errorf("circular dependency detected involving migrations: %v", cycleMigrations)
	}

	return sorted, nil
}

// Constants for Pebble key prefixes
const (
	SchemaVersionKey = "__schema_version__"
	MigrationPrefix  = "__migration_"
)
