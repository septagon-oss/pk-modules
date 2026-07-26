// Package postgres is the Postgres SessionStore implementation for
// auth_management backed by database/sql.
//
// postgres.go is the production sibling of store/sqlite: the same Store
// contract and the same query shapes — only the SQL dialect differs ($N
// placeholders, TIMESTAMPTZ columns, SQLSTATE-based error classification). Note
// what does NOT change: sessions are looked up by their opaque id, which is the
// bearer secret, so Get/Revoke are keyed on id alone with no tenant predicate —
// the id itself authorizes and the row carries its tenant. Callers register a
// Postgres driver (typically `_ "github.com/jackc/pgx/v5/stdlib"`, driver name
// "pgx") before opening a database; this package imports no driver so the binary
// stays slim and the driver is the host application's build-time choice.
//
// ADR: ADR-0009 (ports-only module communication), ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package postgres

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

// Store is a database/sql-backed implementation of the auth SessionStore for
// Postgres. It is safe for concurrent use by multiple goroutines.
type Store struct {
	db *sql.DB
}

// New returns a Store wrapping the given *sql.DB and ensures the
// `auth_sessions` table exists.
func New(db *sql.DB) (*Store, error) {
	if db == nil {
		return nil, errors.New("auth/postgres: nil *sql.DB")
	}
	s := &Store{db: db}
	if err := s.ensureSchema(context.Background()); err != nil {
		return nil, err
	}
	return s, nil
}

// Open opens a Postgres database with the given driverName and DSN and returns
// a Store.
func Open(driverName, dsn string) (*Store, error) {
	db, err := sql.Open(driverName, dsn)
	if err != nil {
		return nil, fmt.Errorf("auth/postgres: open: %w", err)
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
    issued_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_auth_sessions_user ON auth_sessions(user_id);
CREATE INDEX IF NOT EXISTS idx_auth_sessions_tenant ON auth_sessions(tenant_id);
`

func (s *Store) ensureSchema(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, schemaDDL); err != nil {
		return fmt.Errorf("auth/postgres: ensure schema: %w", err)
	}
	return nil
}

const selectColumns = `SELECT id, user_id, tenant_id, issued_at, expires_at, revoked_at FROM auth_sessions`

// Create inserts a session. Returns store.ErrDuplicate when the primary key
// collides.
func (s *Store) Create(ctx context.Context, sess *store.Session) error {
	if sess == nil {
		return errors.New("auth/postgres: nil session")
	}
	if sess.IssuedAt.IsZero() {
		sess.IssuedAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(
		ctx,
		`INSERT INTO auth_sessions (id, user_id, tenant_id, issued_at, expires_at, revoked_at)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		sess.ID, sess.UserID, sess.TenantID, sess.IssuedAt, sess.ExpiresAt, nullableTime(sess.RevokedAt),
	)
	if err != nil {
		return classifyInsertErr(err)
	}
	return nil
}

// Get returns a session by ID. There is deliberately NO tenant predicate: the
// session id is an opaque, unguessable bearer secret, so possessing it is the
// authorization, and the returned row carries the tenant it belongs to. Adding
// a tenant_id filter here would be circular — the caller has no authenticated
// tenant until this very lookup resolves the session that establishes one.
func (s *Store) Get(ctx context.Context, id string) (*store.Session, error) {
	row := s.db.QueryRowContext(ctx, selectColumns+` WHERE id = $1`, id)
	sess, err := scanSession(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, store.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("auth/postgres: scan session: %w", err)
	}
	return sess, nil
}

// Revoke marks a single session revoked at time.Now().UTC(). Like Get, it is
// keyed on the session id alone (plus the live-row guard revoked_at IS NULL):
// the id is the capability, so the holder may retire it without proving a
// tenant. A no-op update is disambiguated into ErrNotFound versus
// already-revoked by re-reading the row.
func (s *Store) Revoke(ctx context.Context, id string) error {
	now := time.Now().UTC()
	res, err := s.db.ExecContext(
		ctx,
		`UPDATE auth_sessions SET revoked_at = $1 WHERE id = $2 AND revoked_at IS NULL`,
		now, id,
	)
	if err != nil {
		return fmt.Errorf("auth/postgres: revoke: %w", err)
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

// RevokeByUser marks all live sessions belonging to userID as revoked. The
// predicate is user_id (plus the live-row guard), not tenant_id: this is the
// "log this user out everywhere" operation and a user belongs to exactly one
// tenant carried on each row, so scoping by user is both necessary and
// sufficient.
func (s *Store) RevokeByUser(ctx context.Context, userID string) error {
	now := time.Now().UTC()
	_, err := s.db.ExecContext(
		ctx,
		`UPDATE auth_sessions SET revoked_at = $1 WHERE user_id = $2 AND revoked_at IS NULL`,
		now, userID,
	)
	if err != nil {
		return fmt.Errorf("auth/postgres: revoke by user: %w", err)
	}
	return nil
}

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

// classifyInsertErr maps a Postgres insert error onto the auth store's
// sentinels. auth_sessions carries a single unique constraint — the primary key
// on id — so Postgres reports a duplicate session id as SQLSTATE 23505
// (unique_violation) naming the constraint auth_sessions_pkey. Recognising the
// _pkey constraint is what distinguishes a duplicate id; there is no second
// unique constraint (no ErrSlugTaken analogue on this table), so any other
// unique violation still maps to ErrDuplicate — exactly the sqlite adapter's
// sentinel mapping, which returns ErrDuplicate for every unique violation.
func classifyInsertErr(err error) error {
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "auth_sessions_pkey") {
		return store.ErrDuplicate
	}
	if isUniqueViolation(err) {
		return store.ErrDuplicate
	}
	return fmt.Errorf("auth/postgres: insert session: %w", err)
}

// isUniqueViolation reports whether err is a Postgres unique-constraint
// violation. pgx renders SQLSTATE 23505 with the text "duplicate key value
// violates unique constraint", so matching "unique" (or the bare SQLSTATE) is
// precise here and does not catch NOT NULL / CHECK / FK failures.
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique") || strings.Contains(msg, "23505")
}
