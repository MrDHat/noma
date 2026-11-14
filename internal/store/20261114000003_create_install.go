package store

import (
	"context"

	"github.com/uptrace/bun"
)

// The install has one row. Creates take it FOR SHARE and the close takes
// it FOR UPDATE, so the two never interleave (M3.5-19, verify/teardown).
func init() {
	migrations.MustRegister(func(ctx context.Context, db *bun.DB) error {
		_, err := db.ExecContext(ctx, `
CREATE TABLE install (
  id      int PRIMARY KEY CHECK (id = 1),
  closing boolean NOT NULL DEFAULT false
);
INSERT INTO install (id) VALUES (1);`)
		return err
	}, nil)
}
