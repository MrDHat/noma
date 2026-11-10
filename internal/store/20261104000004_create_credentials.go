package store

import (
	"context"
	"time"

	"github.com/uptrace/bun"
)

func init() {
	type credential struct {
		bun.BaseModel `bun:"table:credentials"`

		ID         string    `bun:",pk"`
		OwnerKind  string    `bun:",notnull"`
		Owner      string    `bun:",notnull"`
		Name       string    `bun:",notnull"`
		GitHost    string    `bun:",notnull"`
		EnvVar     string    `bun:",notnull"`
		Repos      []string  `bun:",array,notnull"`
		KeyID      string    `bun:",notnull"`
		Ciphertext []byte    `bun:",notnull"`
		CreatedAt  time.Time `bun:",notnull,default:current_timestamp"`
		UpdatedAt  time.Time `bun:",notnull,default:current_timestamp"`
	}
	migrations.MustRegister(func(ctx context.Context, db *bun.DB) error {
		if _, err := db.NewCreateTable().Model((*credential)(nil)).Exec(ctx); err != nil {
			return err
		}
		_, err := db.NewCreateIndex().Model((*credential)(nil)).
			Index("credentials_owner_name").Unique().Column("owner_kind", "owner", "name").Exec(ctx)
		return err
	}, nil)
}
