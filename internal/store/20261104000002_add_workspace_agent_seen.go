package store

import (
	"context"

	"github.com/uptrace/bun"
)

func init() {
	migrations.MustRegister(func(ctx context.Context, db *bun.DB) error {
		_, err := db.NewAddColumn().Table("workspaces").ColumnExpr("agent_seen_at timestamptz").Exec(ctx)
		return err
	}, nil)
}
