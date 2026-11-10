package store

import (
	"context"
	"time"

	"github.com/uptrace/bun"
)

// Owner kinds of a credential (M3-10). M3 builds only user-owned ones; the
// column is there so organisation rules need no migration.
const OwnerUser = "user"

// Credential is one rule: whose it is, which workspaces it reaches, how it
// is delivered, and its value sealed under the server key KeyID.
type Credential struct {
	bun.BaseModel `bun:"table:credentials"`

	ID        string `bun:",pk"`
	OwnerKind string `bun:",notnull"`
	Owner     string `bun:",notnull"`
	Name      string `bun:",notnull"`
	// GitHost and EnvVar say how it is delivered: as a git credential for
	// the host, as an environment variable, or both.
	GitHost string `bun:",notnull"`
	EnvVar  string `bun:",notnull"`
	// Repos limits it to repos matching these patterns; empty means all of
	// the owner's workspaces.
	Repos      []string  `bun:",array,notnull"`
	KeyID      string    `bun:",notnull"`
	Ciphertext []byte    `bun:",notnull"`
	CreatedAt  time.Time `bun:",notnull,default:current_timestamp"`
	UpdatedAt  time.Time `bun:",notnull,default:current_timestamp"`
}

// PutCredential creates the credential, or replaces the owner's credential
// of the same name.
func (s *Store) PutCredential(ctx context.Context, c *Credential) error {
	if c.Repos == nil {
		c.Repos = []string{}
	}
	_, err := s.db.NewInsert().Model(c).
		On("CONFLICT (owner_kind, owner, name) DO UPDATE").
		Set("git_host = EXCLUDED.git_host").
		Set("env_var = EXCLUDED.env_var").
		Set("repos = EXCLUDED.repos").
		Set("key_id = EXCLUDED.key_id").
		Set("ciphertext = EXCLUDED.ciphertext").
		Set("updated_at = now()").
		Exec(ctx)
	return err
}

// Credentials returns the owner's credentials by name.
func (s *Store) Credentials(ctx context.Context, ownerKind, owner string) ([]Credential, error) {
	var cs []Credential
	err := s.db.NewSelect().Model(&cs).
		Where("owner_kind = ? AND owner = ?", ownerKind, owner).Order("name").Scan(ctx)
	return cs, err
}

// DeleteCredential removes the owner's credential called name.
func (s *Store) DeleteCredential(ctx context.Context, ownerKind, owner, name string) error {
	res, err := s.db.NewDelete().Model((*Credential)(nil)).
		Where("owner_kind = ? AND owner = ? AND name = ?", ownerKind, owner, name).Exec(ctx)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
