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
	DesiredStopped = "stopped"
	DesiredDeleted = "deleted"
)

// ErrNotStopped is returned when starting a workspace that has not
// finished stopping.
var ErrNotStopped = errors.New("workspace is not stopped")

// Workspace is one row of the workspaces table.
type Workspace struct {
	bun.BaseModel `bun:"table:workspaces"`

	ID      string `bun:",pk"`
	Owner   string `bun:",notnull"`
	Name    string `bun:",notnull"`
	RepoURL string `bun:",notnull"`
	Desired string `bun:",notnull"`
	Phase   string `bun:",notnull"`
	Message string `bun:",notnull"`
	// Link is the Remote Control link the agent reported.
	Link string `bun:",notnull"`
	// SignInURL and SignInKey describe a Claude sign-in waiting for a code:
	// the URL to open and the agent's one-time public key to seal it with.
	SignInURL string `bun:"signin_url,notnull"`
	SignInKey string `bun:"signin_key,notnull"`
	// Image is the project image the repo's noma.yaml names, or "" for the
	// Noma workspace image.
	Image string `bun:",notnull"`
	// ClaudeRestarts and ClaudeLastExit are the agent's last report on Claude.
	ClaudeRestarts int    `bun:",notnull"`
	ClaudeLastExit string `bun:",notnull"`
	// Boot is the boot of the agent that last reported. Each agent counts
	// its start on the workspace volume, so a later agent has a higher one.
	Boot int64 `bun:",notnull"`
	// AgentSeenAt is when the agent last polled, kept to the minute.
	AgentSeenAt time.Time `bun:",nullzero"`
	CreatedAt   time.Time `bun:",notnull,default:current_timestamp"`
	UpdatedAt   time.Time `bun:",notnull,default:current_timestamp"`
}

// User is one row of the users table: what Noma keeps per user.
type User struct {
	bun.BaseModel `bun:"table:users"`

	Name string `bun:",pk"`
	// GitName and GitEmail are the identity of the user's commits.
	GitName  string `bun:",notnull"`
	GitEmail string `bun:",notnull"`
	// ClaudeAccount is the Claude account the user's shared login must
	// belong to, pinned the first time a workspace checks it (M3-26).
	ClaudeAccount string    `bun:",notnull"`
	CreatedAt     time.Time `bun:",notnull,default:current_timestamp"`
	UpdatedAt     time.Time `bun:",notnull,default:current_timestamp"`
}

// SetGitIdentity records the user's git identity, creating the user's row
// if needed.
func (s *Store) SetGitIdentity(ctx context.Context, user, name, email string) error {
	_, err := s.db.NewInsert().Model(&User{Name: user, GitName: name, GitEmail: email}).
		On("CONFLICT (name) DO UPDATE").
		Set("git_name = EXCLUDED.git_name").
		Set("git_email = EXCLUDED.git_email").
		Set("updated_at = now()").
		Exec(ctx)
	return err
}

// PinClaudeAccount pins account as the user's Claude account if none is
// pinned yet, and says whether account is the pinned one.
func (s *Store) PinClaudeAccount(ctx context.Context, user, account string) (bool, error) {
	_, err := s.db.NewInsert().Model(&User{Name: user, ClaudeAccount: account}).
		On("CONFLICT (name) DO UPDATE").
		Set("claude_account = EXCLUDED.claude_account").
		Set("updated_at = now()").
		Where("?TableAlias.claude_account = ''").
		Exec(ctx)
	if err != nil {
		return false, err
	}
	u, err := s.UserByName(ctx, user)
	if err != nil {
		return false, err
	}
	return u.ClaudeAccount == account, nil
}

// UserByName returns the user's row, or ErrNotFound.
func (s *Store) UserByName(ctx context.Context, name string) (*User, error) {
	u := new(User)
	err := s.db.NewSelect().Model(u).Where("name = ?", name).Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return u, err
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

// WorkspacesOf returns the owner's workspaces, oldest first.
func (s *Store) WorkspacesOf(ctx context.Context, owner string) ([]Workspace, error) {
	var ws []Workspace
	err := s.db.NewSelect().Model(&ws).Where("owner = ?", owner).Order("created_at").Scan(ctx)
	return ws, err
}

// MarkDeleted records that the user wants the workspace gone, running or
// stopped. The reconciler removes its Kubernetes objects and then the row.
func (s *Store) MarkDeleted(ctx context.Context, id string) error {
	_, err := s.setDesired(DesiredDeleted, api.PhaseDeleting).
		Where("id = ? AND desired <> ?", id, DesiredDeleted).Exec(ctx)
	return err
}

// MarkStopped records that the user wants a running workspace stopped. The
// reconciler scales it to no pod and keeps its volume. A workspace that is
// not running is left as it is.
func (s *Store) MarkStopped(ctx context.Context, id string) error {
	_, err := s.setDesired(DesiredStopped, api.PhaseStopping).
		Where("id = ? AND desired = ?", id, DesiredRunning).Exec(ctx)
	return err
}

// MarkStarted records that the user wants a stopped workspace running
// again. It returns ErrNotStopped unless the workspace has finished
// stopping: the old pod's agent has already reported, so a workspace
// started while that pod still runs would never hear from it again and
// stay "starting" (found by the lifecycle model; decision M3-25).
func (s *Store) MarkStarted(ctx context.Context, id string) error {
	res, err := s.setDesired(DesiredRunning, api.PhaseStarting).
		Where("id = ? AND desired = ? AND phase = ?", id, DesiredStopped, api.PhaseStopped).Exec(ctx)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotStopped
	}
	return nil
}

func (s *Store) setDesired(desired, phase string) *bun.UpdateQuery {
	return s.db.NewUpdate().Model((*Workspace)(nil)).
		Set("desired = ?", desired).
		Set("phase = ?", phase).
		Set("message = ''").
		Set("updated_at = now()")
}

// AgentStatus is what one agent report records.
type AgentStatus struct {
	// Boot is the reporting agent's boot.
	Boot           int64
	Phase, Message string
	// SignInURL and SignInKey are empty unless the phase is sign-in.
	SignInURL, SignInKey string
	// Link is left as it is when empty.
	Link           string
	ClaudeRestarts int
	ClaudeLastExit string
}

// SetStatus records an agent's report in one statement. It leaves rows
// that are being deleted or stopped alone, so a late report can't undo a
// delete. It also leaves the row alone when a later agent has reported: a
// report in flight when its container crashed must not overwrite the new
// agent's (NOM-70).
func (s *Store) SetStatus(ctx context.Context, id string, st AgentStatus) error {
	q := s.setPhase(st.Phase, st.Message).
		Set("signin_url = ?", st.SignInURL).
		Set("signin_key = ?", st.SignInKey).
		Set("claude_restarts = ?", st.ClaudeRestarts).
		Set("claude_last_exit = ?", st.ClaudeLastExit).
		Set("boot = ?", st.Boot)
	if st.Link != "" {
		q = q.Set("link = ?", st.Link)
	}
	_, err := q.Where("id = ? AND desired = ? AND boot <= ?", id, DesiredRunning, st.Boot).Exec(ctx)
	return err
}

// SetImage records the project image the agent found in noma.yaml, with
// SetStatus's guard on the agent's boot. It is not a phase change:
// updated_at stays, so the reconciler's phase writes are not refused for it.
func (s *Store) SetImage(ctx context.Context, id string, boot int64, image string) error {
	_, err := s.db.NewUpdate().Model((*Workspace)(nil)).
		Set("image = ?", image).
		Set("boot = ?", boot).
		Where("id = ? AND desired = ? AND boot <= ?", id, DesiredRunning, boot).Exec(ctx)
	return err
}

// SetAgentSeen records that the workspace's agent polled just now. It
// leaves updated_at alone: a poll is not a change the reconciler must see.
func (s *Store) SetAgentSeen(ctx context.Context, id string) error {
	_, err := s.db.NewUpdate().Model((*Workspace)(nil)).
		Set("agent_seen_at = now()").
		Where("id = ? AND desired = ?", id, DesiredRunning).Exec(ctx)
	return err
}

// SetPhaseIfUnchanged records a phase the reconciler chose from a row it
// read earlier. It writes only if nothing has changed the row's phase or
// desired state since, so the reconciler never overwrites a phase it has
// not seen.
func (s *Store) SetPhaseIfUnchanged(ctx context.Context, read *Workspace, phase, message string) error {
	_, err := s.setPhase(phase, message).
		Where("id = ? AND desired = ? AND updated_at = ?", read.ID, read.Desired, read.UpdatedAt).Exec(ctx)
	return err
}

func (s *Store) setPhase(phase, message string) *bun.UpdateQuery {
	return s.db.NewUpdate().Model((*Workspace)(nil)).
		Set("phase = ?", phase).
		Set("message = ?", message).
		Set("updated_at = now()")
}

// ErrLocked is returned when another noma-server holds the reconcile lock.
var ErrLocked = errors.New("another noma-server is reconciling")

// reconcileLock is the key of the Postgres advisory lock that reconcile
// passes hold. It spells "noma".
const reconcileLock = 0x6e6f6d61

// WithReconcileLock runs fn while it holds the reconcile lock, so passes on
// different noma-server replicas never overlap. If another replica holds the
// lock, it returns ErrLocked without running fn. The lock belongs to a
// transaction that does nothing else, so Postgres frees it when fn returns,
// ctx ends or the connection drops.
func (s *Store) WithReconcileLock(ctx context.Context, fn func() error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var locked bool
	if err := tx.QueryRowContext(ctx, "SELECT pg_try_advisory_xact_lock(?)", reconcileLock).Scan(&locked); err != nil {
		return fmt.Errorf("take reconcile lock: %w", err)
	}
	if !locked {
		return ErrLocked
	}
	return fn()
}

// WorkspaceByID returns the workspace with the given ID.
func (s *Store) WorkspaceByID(ctx context.Context, id string) (*Workspace, error) {
	w := new(Workspace)
	err := s.db.NewSelect().Model(w).Where("id = ?", id).Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return w, err
}
