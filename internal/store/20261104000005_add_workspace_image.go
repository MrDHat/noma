package store

import (
	"context"

	"github.com/uptrace/bun"
)

func init() {
	migrations.MustRegister(func(ctx context.Context, db *bun.DB) error {
		_, err := db.NewAddColumn().Table("workspaces").ColumnExpr("image text NOT NULL DEFAULT ''").Exec(ctx)
		return err
	}, nil)
}
