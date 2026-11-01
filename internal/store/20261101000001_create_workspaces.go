package store

import (
	"context"
	"time"

	"github.com/uptrace/bun"
)

func init() {
	// The table shape is frozen here rather than taken from Workspace, so
	// later changes to the model need their own migration.
	type workspace struct {
		bun.BaseModel `bun:"table:workspaces"`

		ID        string    `bun:",pk"`
		Owner     string    `bun:",notnull"`
		Name      string    `bun:",notnull"`
		RepoURL   string    `bun:",notnull"`
		Desired   string    `bun:",notnull"`
		Phase     string    `bun:",notnull"`
		Message   string    `bun:",notnull"`
		CreatedAt time.Time `bun:",notnull,default:current_timestamp"`
		UpdatedAt time.Time `bun:",notnull,default:current_timestamp"`
	}
	migrations.MustRegister(func(ctx context.Context, db *bun.DB) error {
		if _, err := db.NewCreateTable().Model((*workspace)(nil)).Exec(ctx); err != nil {
			return err
		}
		_, err := db.NewCreateIndex().Model((*workspace)(nil)).
			Index("workspaces_owner_name").Unique().Column("owner", "name").Exec(ctx)
		return err
	}, nil)
}
