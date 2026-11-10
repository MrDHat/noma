package store

import (
	"context"
	"time"

	"github.com/uptrace/bun"
)

func init() {
	type user struct {
		bun.BaseModel `bun:"table:users"`

		Name      string    `bun:",pk"`
		GitName   string    `bun:",notnull"`
		GitEmail  string    `bun:",notnull"`
		CreatedAt time.Time `bun:",notnull,default:current_timestamp"`
		UpdatedAt time.Time `bun:",notnull,default:current_timestamp"`
	}
	migrations.MustRegister(func(ctx context.Context, db *bun.DB) error {
		_, err := db.NewCreateTable().Model((*user)(nil)).Exec(ctx)
		return err
	}, nil)
}
