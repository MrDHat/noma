package store

import (
	"context"

	"github.com/uptrace/bun"
)

func init() {
	migrations.MustRegister(func(ctx context.Context, db *bun.DB) error {
		for _, col := range []string{"signin_url text NOT NULL DEFAULT ''", "signin_key text NOT NULL DEFAULT ''"} {
			if _, err := db.NewAddColumn().Table("workspaces").ColumnExpr(col).Exec(ctx); err != nil {
				return err
			}
		}
		return nil
	}, nil)
}
