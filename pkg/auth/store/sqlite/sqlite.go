// Package sqlite is the default SessionStore implementation for
// auth_management backed by database/sql.
//
// sqlite.go owns the Store implementation. Callers must register their
// preferred sqlite driver (typically `_ "modernc.org/sqlite"`) before
// opening a database; this package does not import any driver so binaries
// stay slim and the driver is a build-time concern of the host application.
//
// ADR: ADR-0009 (ports-only module communication), ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package sqlite

// Implements: REQ-AUTH-001.
// Per: ADR-0028.
// Discipline: C-14.
import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/septagon-oss/pk-modules/pkg/auth/store"
)

// Store is a database/sql-backed implementation of the auth SessionStore.
// It is safe for concurrent use by multiple goroutines.
type Store struct {
	db *sql.DB
}

// New returns a Store wrapping the given *sql.DB and ensures the
// `auth_sessions` table exists so callers can pair a fresh in-memory
// database with the store without running the migration files explicitly.
func New(db *sql.DB) (*Store, error) {
	if db == nil {
		return nil, errors.New("auth/sqlite: nil *sql.DB")
	}
	s := &Store{db: db}
	if err := s.ensureSchema(context.Background()); err != nil {
		return nil, err
	}
	return s, nil
}

// Open opens a sqlite database with the given driverName and DSN and
// returns a Store.
func Open(driverName, dsn string) (*Store, error) {
	db, err := sql.Open(driverName, dsn)
	if err != nil {
		return nil, fmt.Errorf("auth/sqlite: open: %w", err)
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
CREATE TABLE IF NOT EXISTS auth_sessions (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL,
    tenant_id TEXT NOT NULL,
    issued_at DATETIME NOT NULL,
    expires_at DATETIME NOT NULL,
    revoked_at DATETIME
);
CREATE INDEX IF NOT EXISTS idx_auth_sessions_user ON auth_sessions(user_id);
CREATE INDEX IF NOT EXISTS idx_auth_sessions_tenant ON auth_sessions(tenant_id);
`

func (s *Store) ensureSchema(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, schemaDDL); err != nil {
		return fmt.Errorf("auth/sqlite: ensure schema: %w", err)
	}
	return nil
}

// Create inserts a session. Returns store.ErrDuplicate if the primary key
// collides.
func (s *Store) Create(ctx context.Context, sess *store.Session) error {
	if sess == nil {
		return errors.New("auth/sqlite: nil session")
	}
	if sess.IssuedAt.IsZero() {
		sess.IssuedAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(
		ctx,
		`INSERT INTO auth_sessions (id, user_id, tenant_id, issued_at, expires_at, revoked_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		sess.ID, sess.UserID, sess.TenantID, sess.IssuedAt, sess.ExpiresAt, nullableTime(sess.RevokedAt),
	)
	if err != nil {
		if isUniqueViolation(err) {
			return store.ErrDuplicate
		}
		return fmt.Errorf("auth/sqlite: insert session: %w", err)
	}
	return nil
}

// Get returns a session by ID.
func (s *Store) Get(ctx context.Context, id string) (*store.Session, error) {
	row := s.db.QueryRowContext(ctx, selectColumns+` WHERE id = ?`, id)
	sess, err := scanSession(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, store.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("auth/sqlite: scan session: %w", err)
	}
	return sess, nil
}

// Revoke marks a single session revoked at time.Now().UTC().
func (s *Store) Revoke(ctx context.Context, id string) error {
	now := time.Now().UTC()
	res, err := s.db.ExecContext(
		ctx,
		`UPDATE auth_sessions SET revoked_at = ? WHERE id = ? AND revoked_at IS NULL`,
		now, id,
	)
	if err != nil {
		return fmt.Errorf("auth/sqlite: revoke: %w", err)
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		// The session may already be revoked, or not exist. Disambiguate.
		if _, getErr := s.Get(ctx, id); errors.Is(getErr, store.ErrNotFound) {
			return store.ErrNotFound
		}
	}
	return nil
}

// RevokeByUser marks all live sessions belonging to userID as revoked.
func (s *Store) RevokeByUser(ctx context.Context, userID string) error {
	now := time.Now().UTC()
	_, err := s.db.ExecContext(
		ctx,
		`UPDATE auth_sessions SET revoked_at = ? WHERE user_id = ? AND revoked_at IS NULL`,
		now, userID,
	)
	if err != nil {
		return fmt.Errorf("auth/sqlite: revoke by user: %w", err)
	}
	return nil
}

const selectColumns = `SELECT id, user_id, tenant_id, issued_at, expires_at, revoked_at FROM auth_sessions`

type scannable interface {
	Scan(dest ...any) error
}

func scanSession(row scannable) (*store.Session, error) {
	sess := &store.Session{}
	var revoked sql.NullTime
	if err := row.Scan(&sess.ID, &sess.UserID, &sess.TenantID, &sess.IssuedAt, &sess.ExpiresAt, &revoked); err != nil {
		return nil, err
	}
	if revoked.Valid {
		t := revoked.Time
		sess.RevokedAt = &t
	}
	return sess, nil
}

func nullableTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return *t
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	// Match only UNIQUE violations. sqlite phrases them "UNIQUE constraint
	// failed: ..." and Postgres "violates unique constraint"; both contain
	// "unique". Matching bare "constraint" was too broad — it mislabeled NOT
	// NULL/CHECK/FK failures as duplicates (a spurious 409).
	return strings.Contains(msg, "unique")
}
