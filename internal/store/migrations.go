package store

import (
	"context"
	"fmt"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/migrate"
)

// migrations holds every schema change. Each one lives in its own file named
// <timestamp>_<what>.go, which Bun uses as the migration's name.
var migrations = migrate.NewMigrations()

func runMigrations(ctx context.Context, db *bun.DB) error {
	m := migrate.NewMigrator(db, migrations)
	if err := m.Init(ctx); err != nil {
		return fmt.Errorf("init migrations: %w", err)
	}
	if err := m.Lock(ctx); err != nil {
		return fmt.Errorf("lock migrations: %w", err)
	}
	defer m.Unlock(ctx) //nolint:errcheck
	if _, err := m.Migrate(ctx); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	return nil
}
