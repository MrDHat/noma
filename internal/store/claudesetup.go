package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/uptrace/bun"
)

// ClaudeSetup is a user's latest Claude setup from their laptop (M3.5-13):
// CLAUDE.md, skills and settings.json as a tar.gz. It is stored as sent,
// not sealed: it holds instructions, not secrets (decision M3.5-13).
type ClaudeSetup struct {
	bun.BaseModel `bun:"table:claude_setups"`

	Owner     string    `bun:",pk"`
	Archive   []byte    `bun:",notnull"`
	UpdatedAt time.Time `bun:",notnull,default:current_timestamp"`
}

// PutClaudeSetup replaces the owner's Claude setup.
func (s *Store) PutClaudeSetup(ctx context.Context, owner string, archive []byte) error {
	_, err := s.db.NewInsert().Model(&ClaudeSetup{Owner: owner, Archive: archive}).
		On("CONFLICT (owner) DO UPDATE").
		Set("archive = EXCLUDED.archive").
		Set("updated_at = now()").
		Exec(ctx)
	return err
}

// ClaudeSetupFor returns the owner's Claude setup, or ErrNotFound.
func (s *Store) ClaudeSetupFor(ctx context.Context, owner string) (*ClaudeSetup, error) {
	cs := new(ClaudeSetup)
	err := s.db.NewSelect().Model(cs).Where("owner = ?", owner).Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return cs, err
}
