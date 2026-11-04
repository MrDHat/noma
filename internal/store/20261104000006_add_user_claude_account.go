package store

import (
	"context"

	"github.com/uptrace/bun"
)

func init() {
	migrations.MustRegister(func(ctx context.Context, db *bun.DB) error {
		_, err := db.ExecContext(ctx, `ALTER TABLE users ADD COLUMN claude_account text NOT NULL DEFAULT ''`)
		return err
	}, nil)
}
