// Package sqlite is the default Store implementation for user_management
// backed by database/sql.
//
// sqlite.go owns the Store implementation. Callers must register their
// preferred sqlite driver (typically `_ "modernc.org/sqlite"`) before opening
// a database; this package does not import any driver so binaries stay slim
// and the driver is a build-time concern of the host application.
//
// ADR: ADR-0009 (ports-only module communication), ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package sqlite

// Implements: REQ-USER-001.
// Per: ADR-0017.
// Discipline: C-14.
import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/septagon-oss/pk-modules/pkg/user/store"
)

// Store is a database/sql-backed implementation of store.Store. It is safe
// for concurrent use by multiple goroutines.
// Compile-time proof the sqlite store satisfies the store.Store contract
// (Effective Go "interface checks").
var _ store.Store = (*Store)(nil)

type Store struct {
	db *sql.DB
}

// New returns a Store wrapping the given *sql.DB. It also ensures the
// `users` table exists, so callers can pair a fresh in-memory database with
// the store without running the migration files explicitly.
func New(db *sql.DB) (*Store, error) {
	if db == nil {
		return nil, errors.New("user/sqlite: nil *sql.DB")
	}
	s := &Store{db: db}
	if err := s.ensureSchema(context.Background()); err != nil {
		return nil, err
	}
	return s, nil
}

// Open opens a sqlite database with the given driverName and DSN and returns
// a Store. Caller is responsible for closing the returned *sql.DB via DB().
func Open(driverName, dsn string) (*Store, error) {
	db, err := sql.Open(driverName, dsn)
	if err != nil {
		return nil, fmt.Errorf("user/sqlite: open: %w", err)
	}
	s, err := New(db)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// DB exposes the underlying *sql.DB so the caller controls lifecycle.
func (s *Store) DB() *sql.DB { return s.db }

const schemaDDL = `
CREATE TABLE IF NOT EXISTS users (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    email TEXT NOT NULL,
    username TEXT NOT NULL,
    pass_hash TEXT NOT NULL DEFAULT '',
    display_name TEXT NOT NULL DEFAULT '',
    active INTEGER NOT NULL DEFAULT 1,
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS uq_users_tenant_email ON users(tenant_id, email);
CREATE UNIQUE INDEX IF NOT EXISTS uq_users_tenant_username ON users(tenant_id, username);
CREATE INDEX IF NOT EXISTS idx_users_tenant ON users(tenant_id);
`

func (s *Store) ensureSchema(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, schemaDDL); err != nil {
		return fmt.Errorf("user/sqlite: ensure schema: %w", err)
	}
	return nil
}

// Create inserts a user. Returns store.ErrDuplicateEmail or
// store.ErrDuplicateUsername if the tenant-scoped uniqueness is violated.
func (s *Store) Create(ctx context.Context, u *store.User) error {
	if u == nil {
		return errors.New("user/sqlite: nil user")
	}
	now := time.Now().UTC()
	if u.CreatedAt.IsZero() {
		u.CreatedAt = now
	}
	if u.UpdatedAt.IsZero() {
		u.UpdatedAt = now
	}
	_, err := s.db.ExecContext(
		ctx,
		`INSERT INTO users (id, tenant_id, email, username, pass_hash, display_name, active, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		u.ID, u.TenantID, u.Email, u.Username, u.PassHash, u.DisplayName, boolToInt(u.Active), u.CreatedAt, u.UpdatedAt,
	)
	if err != nil {
		return classifyUniqueError(err)
	}
	return nil
}

// Get returns a user by ID.
func (s *Store) Get(ctx context.Context, tenantID, id string) (*store.User, error) {
	row := s.db.QueryRowContext(ctx, selectColumns+` WHERE id = ? AND tenant_id = ?`, id, tenantID)
	return scanUser(row)
}

// GetByEmail returns a user by (tenant_id, email).
func (s *Store) GetByEmail(ctx context.Context, tenantID, email string) (*store.User, error) {
	row := s.db.QueryRowContext(ctx, selectColumns+` WHERE tenant_id = ? AND email = ?`, tenantID, email)
	return scanUser(row)
}

// GetByUsername returns a user by (tenant_id, username).
func (s *Store) GetByUsername(ctx context.Context, tenantID, username string) (*store.User, error) {
	row := s.db.QueryRowContext(ctx, selectColumns+` WHERE tenant_id = ? AND username = ?`, tenantID, username)
	return scanUser(row)
}

// List returns users for a tenant, ordered by username.
func (s *Store) List(ctx context.Context, tenantID string, limit, offset int) ([]*store.User, error) {
	const maxLimit = 1000
	if limit <= 0 {
		limit = 50
	}
	if limit > maxLimit {
		// Cap the page size so a caller cannot request an unbounded read of the
		// whole tenant (a memory/latency DoS), matching the other list paths.
		limit = maxLimit
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := s.db.QueryContext(ctx,
		selectColumns+` WHERE tenant_id = ? ORDER BY username LIMIT ? OFFSET ?`,
		tenantID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("user/sqlite: list: %w", err)
	}
	defer rows.Close()
	var out []*store.User
	for rows.Next() {
		u, err := scanUserRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("user/sqlite: list rows: %w", err)
	}
	return out, nil
}

// Update overwrites a user's mutable fields (except pass_hash; use
// UpdatePassHash for credential changes).
func (s *Store) Update(ctx context.Context, u *store.User) error {
	if u == nil {
		return errors.New("user/sqlite: nil user")
	}
	u.UpdatedAt = time.Now().UTC()
	res, err := s.db.ExecContext(
		ctx,
		`UPDATE users SET email = ?, username = ?, display_name = ?, active = ?, updated_at = ? WHERE id = ? AND tenant_id = ?`,
		u.Email, u.Username, u.DisplayName, boolToInt(u.Active), u.UpdatedAt, u.ID, u.TenantID,
	)
	if err != nil {
		return classifyUniqueError(err)
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return store.ErrNotFound
	}
	return nil
}

// UpdatePassHash rewrites the stored pass_hash for the user identified by
// (tenantID, id). The tenant predicate is mandatory so a caller cannot reset
// another tenant's user's password by ID.
func (s *Store) UpdatePassHash(ctx context.Context, tenantID, id, passHash string, updatedAt time.Time) error {
	if updatedAt.IsZero() {
		updatedAt = time.Now().UTC()
	}
	res, err := s.db.ExecContext(
		ctx,
		`UPDATE users SET pass_hash = ?, updated_at = ? WHERE id = ? AND tenant_id = ?`,
		passHash, updatedAt, id, tenantID,
	)
	if err != nil {
		return fmt.Errorf("user/sqlite: update pass_hash: %w", err)
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return store.ErrNotFound
	}
	return nil
}

// Delete removes the user identified by (tenantID, id). The tenant predicate is
// mandatory so a caller cannot delete another tenant's user by ID.
func (s *Store) Delete(ctx context.Context, tenantID, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM users WHERE id = ? AND tenant_id = ?`, id, tenantID)
	if err != nil {
		return fmt.Errorf("user/sqlite: delete: %w", err)
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return store.ErrNotFound
	}
	return nil
}

const selectColumns = `SELECT id, tenant_id, email, username, pass_hash, display_name, active, created_at, updated_at FROM users`

type scannable interface {
	Scan(dest ...any) error
}

func scanUser(row *sql.Row) (*store.User, error) {
	u, err := scanInto(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, store.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("user/sqlite: scan: %w", err)
	}
	return u, nil
}

func scanUserRows(row *sql.Rows) (*store.User, error) {
	u, err := scanInto(row)
	if err != nil {
		return nil, fmt.Errorf("user/sqlite: scan rows: %w", err)
	}
	return u, nil
}

func scanInto(row scannable) (*store.User, error) {
	u := &store.User{}
	var active int
	if err := row.Scan(&u.ID, &u.TenantID, &u.Email, &u.Username, &u.PassHash, &u.DisplayName, &active, &u.CreatedAt, &u.UpdatedAt); err != nil {
		return nil, err
	}
	u.Active = active != 0
	return u, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// classifyUniqueError inspects an error returned by sqlite and maps UNIQUE
// constraint violations to the appropriate sentinel. We match on the error
// message because modernc.org/sqlite exposes errors via an internal type.
// When the message does not name a recognized column we return the generic
// ErrUniqueConstraintViolation rather than guessing — guessing
// "duplicate email" on every unclassified conflict (the original behavior)
// hid real bugs by masking unrelated UNIQUE violations as user-facing email
// duplicates.
func classifyUniqueError(err error) error {
	if err == nil {
		return nil
	}
	msg := strings.ToLower(err.Error())
	if !strings.Contains(msg, "unique") && !strings.Contains(msg, "constraint") {
		return fmt.Errorf("user/sqlite: exec: %w", err)
	}
	switch {
	case strings.Contains(msg, "users.email"), strings.Contains(msg, "uq_users_tenant_email"):
		return store.ErrDuplicateEmail
	case strings.Contains(msg, "users.username"), strings.Contains(msg, "uq_users_tenant_username"):
		return store.ErrDuplicateUsername
	case strings.Contains(msg, "email"):
		return store.ErrDuplicateEmail
	case strings.Contains(msg, "username"):
		return store.ErrDuplicateUsername
	}
	return fmt.Errorf("%w: %v", store.ErrUniqueConstraintViolation, err)
}
