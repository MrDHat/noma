package store

import (
	"context"
	"time"

	"github.com/uptrace/bun"
)

func init() {
	type claudeSetup struct {
		bun.BaseModel `bun:"table:claude_setups"`

		Owner     string    `bun:",pk"`
		Archive   []byte    `bun:",notnull"`
		UpdatedAt time.Time `bun:",notnull,default:current_timestamp"`
	}
	migrations.MustRegister(func(ctx context.Context, db *bun.DB) error {
		_, err := db.NewCreateTable().Model((*claudeSetup)(nil)).Exec(ctx)
		return err
	}, nil)
}
