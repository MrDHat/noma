package store

import (
	"context"

	"github.com/uptrace/bun"
)

func init() {
	migrations.MustRegister(func(ctx context.Context, db *bun.DB) error {
		for _, col := range []string{"claude_restarts integer NOT NULL DEFAULT 0", "claude_last_exit text NOT NULL DEFAULT ''"} {
			if _, err := db.NewAddColumn().Table("workspaces").ColumnExpr(col).Exec(ctx); err != nil {
				return err
			}
		}
		return nil
	}, nil)
}
