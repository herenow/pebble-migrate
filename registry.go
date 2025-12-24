package migrate

import (
	"fmt"
)

// GlobalRegistry is the global migration registry used by the CLI
var GlobalRegistry = NewMigrationRegistry()

// Register is a convenience function to register migrations in the global registry
func Register(m *Migration) error {
	return GlobalRegistry.Register(m)
}

// DiscoveryService handles discovery of migration files
type DiscoveryService struct {
	migrationDir string
	registry     *MigrationRegistry
}

// NewDiscoveryService creates a new discovery service
func NewDiscoveryService(migrationDir string, registry *MigrationRegistry) *DiscoveryService {
	return &DiscoveryService{
		migrationDir: migrationDir,
		registry:     registry,
	}
}

// LoadMigrations discovers and loads all migration files from the migration directory
func (d *DiscoveryService) LoadMigrations() error {
	// For now, we'll use a simpler approach where migrations are registered
	// via init() functions in Go files. This is similar to database/sql drivers.
	// The migration files will be compiled into the binary.

	// In a more advanced implementation, we could:
	// 1. Use Go plugins to dynamically load migrations
	// 2. Parse .sql files with embedded Go code
	// 3. Use reflection to discover migrations

	// For this implementation, migrations are registered via init() functions
	// when the migration files are imported.

	return nil
}

// GetAvailableMigrations returns all registered migrations
func (d *DiscoveryService) GetAvailableMigrations() []*Migration {
	return d.registry.GetMigrations()
}

// ValidateMigrations performs validation on all registered migrations
func (d *DiscoveryService) ValidateMigrations() error {
	migrations := d.registry.GetMigrations()

	if len(migrations) == 0 {
		return fmt.Errorf("no migrations found")
	}

	// Check for duplicate migration IDs
	idMap := make(map[string]bool)
	for _, m := range migrations {
		if idMap[m.ID] {
			return fmt.Errorf("duplicate migration ID found: %s", m.ID)
		}
		idMap[m.ID] = true
	}


	// Validate migration IDs follow naming convention
	for _, m := range migrations {
		if !isValidMigrationID(m.ID) {
			return fmt.Errorf("migration ID '%s' doesn't follow naming convention (should be like '001_description')", m.ID)
		}
	}

	return nil
}

// isValidMigrationID validates that migration ID follows naming convention
func isValidMigrationID(id string) bool {
	// Use the version parsing function to validate format
	_, err := ParseMigrationVersion(id)
	return err == nil
}

// MigrationPlanner helps plan migration execution
type MigrationPlanner struct {
	registry *MigrationRegistry
	schema   *SchemaManager
}

// NewMigrationPlanner creates a new migration planner
func NewMigrationPlanner(registry *MigrationRegistry, schema *SchemaManager) *MigrationPlanner {
	return &MigrationPlanner{
		registry: registry,
		schema:   schema,
	}
}

// PlanUpgrade creates an execution plan to apply all pending migrations
func (p *MigrationPlanner) PlanUpgrade() (*ExecutionPlan, error) {
	currentSchema, err := p.schema.GetSchemaVersion()
	if err != nil {
		return nil, fmt.Errorf("failed to get current schema: %w", err)
	}

	if currentSchema.AppliedMigrations == nil {
		currentSchema.AppliedMigrations = make(map[string]bool)
	}

	pendingMigrations, err := p.registry.GetPendingMigrations(currentSchema.AppliedMigrations)
	if err != nil {
		return nil, fmt.Errorf("failed to get pending migrations: %w", err)
	}

	plan := &ExecutionPlan{
		Type:           ExecutionTypeUpgrade,
		CurrentVersion: currentSchema.CurrentVersion,
		TargetVersion:  currentSchema.CurrentVersion,
		Migrations:     pendingMigrations,
		EstimatedSteps: len(pendingMigrations),
	}

	// Set target version to latest migration's Unix timestamp if any pending
	if len(pendingMigrations) > 0 {
		maxVersion := currentSchema.CurrentVersion
		for _, m := range pendingMigrations {
			if m.Version > maxVersion {
				maxVersion = m.Version
			}
		}
		plan.TargetVersion = maxVersion
	}

	return plan, nil
}

// PlanUpgradeTo creates an execution plan to upgrade to a specific version
func (p *MigrationPlanner) PlanUpgradeTo(targetVersion int64) (*ExecutionPlan, error) {
	currentSchema, err := p.schema.GetSchemaVersion()
	if err != nil {
		return nil, fmt.Errorf("failed to get current schema: %w", err)
	}

	if currentSchema.CurrentVersion >= targetVersion {
		return &ExecutionPlan{
			Type:           ExecutionTypeUpgrade,
			CurrentVersion: currentSchema.CurrentVersion,
			TargetVersion:  currentSchema.CurrentVersion,
			Migrations:     []*Migration{},
			EstimatedSteps: 0,
		}, nil
	}

	if currentSchema.AppliedMigrations == nil {
		currentSchema.AppliedMigrations = make(map[string]bool)
	}

	// Get all migrations up to target version
	allMigrations := p.registry.GetMigrationsInVersionRange(currentSchema.CurrentVersion+1, targetVersion)

	// Filter out already applied migrations
	var pendingMigrations []*Migration
	for _, m := range allMigrations {
		if !currentSchema.AppliedMigrations[m.ID] {
			pendingMigrations = append(pendingMigrations, m)
		}
	}

	return &ExecutionPlan{
		Type:           ExecutionTypeUpgrade,
		CurrentVersion: currentSchema.CurrentVersion,
		TargetVersion:  targetVersion,
		Migrations:     pendingMigrations,
		EstimatedSteps: len(pendingMigrations),
	}, nil
}

// PlanDowngrade creates an execution plan to downgrade to a specific version
func (p *MigrationPlanner) PlanDowngrade(targetVersion int64) (*ExecutionPlan, error) {
	currentSchema, err := p.schema.GetSchemaVersion()
	if err != nil {
		return nil, fmt.Errorf("failed to get current schema: %w", err)
	}

	if currentSchema.CurrentVersion <= targetVersion {
		return &ExecutionPlan{
			Type:           ExecutionTypeDowngrade,
			CurrentVersion: currentSchema.CurrentVersion,
			TargetVersion:  currentSchema.CurrentVersion,
			Migrations:     []*Migration{},
			EstimatedSteps: 0,
		}, nil
	}

	if currentSchema.AppliedMigrations == nil {
		currentSchema.AppliedMigrations = make(map[string]bool)
	}

	// Get migrations to rollback (those after target version)
	migrationsToRollback := p.registry.GetMigrationsInVersionRange(targetVersion+1, currentSchema.CurrentVersion)

	// Filter to only applied migrations and reverse order for rollback
	var rollbackMigrations []*Migration
	for i := len(migrationsToRollback) - 1; i >= 0; i-- {
		m := migrationsToRollback[i]
		if currentSchema.AppliedMigrations[m.ID] {
			rollbackMigrations = append(rollbackMigrations, m)
		}
	}

	return &ExecutionPlan{
		Type:           ExecutionTypeDowngrade,
		CurrentVersion: currentSchema.CurrentVersion,
		TargetVersion:  targetVersion,
		Migrations:     rollbackMigrations,
		EstimatedSteps: len(rollbackMigrations),
	}, nil
}

// PlanRerun creates an execution plan to rerun a specific migration
func (p *MigrationPlanner) PlanRerun(migrationID string) (*ExecutionPlan, error) {
	migration, exists := p.registry.GetMigration(migrationID)
	if !exists {
		return nil, fmt.Errorf("migration '%s' not found", migrationID)
	}

	currentSchema, err := p.schema.GetSchemaVersion()
	if err != nil {
		return nil, fmt.Errorf("failed to get current schema version: %w", err)
	}

	plan := &ExecutionPlan{
		Type:           ExecutionTypeRerun,
		CurrentVersion: currentSchema.CurrentVersion,
		TargetVersion:  currentSchema.CurrentVersion, // Version stays the same for rerun
		Migrations:     []*Migration{migration},
		EstimatedSteps: 2, // Down + Up
	}

	return plan, nil
}

// ExecutionPlan represents a planned migration execution
type ExecutionPlan struct {
	Type           ExecutionType `json:"type"`
	CurrentVersion int64         `json:"current_version"`
	TargetVersion  int64         `json:"target_version"`
	Migrations     []*Migration  `json:"migrations"`
	EstimatedSteps int           `json:"estimated_steps"`
}

// ExecutionType represents the type of migration execution
type ExecutionType string

const (
	ExecutionTypeUpgrade   ExecutionType = "upgrade"
	ExecutionTypeDowngrade ExecutionType = "downgrade"
	ExecutionTypeRerun     ExecutionType = "rerun"
)

// String returns a human-readable description of the execution plan
func (p *ExecutionPlan) String() string {
	switch p.Type {
	case ExecutionTypeUpgrade:
		return fmt.Sprintf("Upgrade from version %d to %d (%d migrations)",
			p.CurrentVersion, p.TargetVersion, len(p.Migrations))
	case ExecutionTypeDowngrade:
		return fmt.Sprintf("Downgrade from version %d to %d (%d rollbacks)",
			p.CurrentVersion, p.TargetVersion, len(p.Migrations))
	case ExecutionTypeRerun:
		if len(p.Migrations) > 0 {
			return fmt.Sprintf("Rerun migration '%s'", p.Migrations[0].ID)
		}
		return "Rerun migration"
	default:
		return "Unknown execution plan"
	}
}
