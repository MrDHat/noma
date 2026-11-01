// Package store keeps Noma's state in Postgres through Bun.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/MrDHat/noma/internal/api"
	"time"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/driver/pgdriver"
)

// ErrNotFound is returned when a row does not exist.
var ErrNotFound = errors.New("not found")

// ErrNameTaken is returned when the owner already has a workspace with that name.
var ErrNameTaken = errors.New("name taken")

// Desired states of a workspace.
const (
	DesiredRunning = "running"
	DesiredDeleted = "deleted"
)

// Workspace is one row of the workspaces table.
type Workspace struct {
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

// Store wraps the database.
type Store struct {
	db *bun.DB
}

// Open connects to Postgres and applies pending migrations.
func Open(ctx context.Context, dsn string) (*Store, error) {
	db := bun.NewDB(sql.OpenDB(pgdriver.NewConnector(pgdriver.WithDSN(dsn))), pgdialect.New())
	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("connect to postgres: %w", err)
	}
	if err := runMigrations(ctx, db); err != nil {
		return nil, err
	}
	return &Store{db: db}, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// CreateWorkspace inserts w. It returns ErrNameTaken if the owner already uses w.Name.
func (s *Store) CreateWorkspace(ctx context.Context, w *Workspace) error {
	_, err := s.db.NewInsert().Model(w).Returning("*").Exec(ctx)
	var pgErr pgdriver.Error
	if errors.As(err, &pgErr) && pgErr.IntegrityViolation() {
		return ErrNameTaken
	}
	return err
}

// WorkspaceByName returns the owner's workspace called name.
func (s *Store) WorkspaceByName(ctx context.Context, owner, name string) (*Workspace, error) {
	w := new(Workspace)
	err := s.db.NewSelect().Model(w).Where("owner = ? AND name = ?", owner, name).Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return w, err
}

// DeleteWorkspace removes the row with the given ID.
func (s *Store) DeleteWorkspace(ctx context.Context, id string) error {
	res, err := s.db.NewDelete().Model((*Workspace)(nil)).Where("id = ?", id).Exec(ctx)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// Workspaces returns every workspace, for the reconciler.
func (s *Store) Workspaces(ctx context.Context) ([]Workspace, error) {
	var ws []Workspace
	err := s.db.NewSelect().Model(&ws).Order("created_at").Scan(ctx)
	return ws, err
}

// MarkDeleted records that the user wants the workspace gone. The reconciler
// removes its Kubernetes objects and then the row.
func (s *Store) MarkDeleted(ctx context.Context, id string) error {
	_, err := s.db.NewUpdate().Model((*Workspace)(nil)).
		Set("desired = ?", DesiredDeleted).
		Set("phase = ?", api.PhaseDeleting).
		Set("message = ''").
		Set("updated_at = now()").
		Where("id = ?", id).Exec(ctx)
	return err
}

// SetObserved records what the reconciler saw. It leaves rows that are being
// deleted alone, so a late update can't undo a delete.
func (s *Store) SetObserved(ctx context.Context, id, phase, message string) error {
	_, err := s.db.NewUpdate().Model((*Workspace)(nil)).
		Set("phase = ?", phase).
		Set("message = ?", message).
		Set("updated_at = now()").
		Where("id = ? AND desired = ?", id, DesiredRunning).Exec(ctx)
	return err
}
