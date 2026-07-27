// Package postgres is the Postgres Store implementation for user_management
// backed by database/sql.
//
// postgres.go is the production sibling of store/sqlite: the same Store
// contract, the same mandatory tenant predicate on every by-ID and list query,
// the same tenant-scoped email/username uniqueness — only the SQL dialect
// differs ($N placeholders, TIMESTAMPTZ columns, SQLSTATE-based error
// classification). Callers register a Postgres driver (typically
// `_ "github.com/jackc/pgx/v5/stdlib"`, driver name "pgx") before opening a
// database; this package imports no driver so the binary stays slim and the
// driver is the host application's build-time choice.
//
// ADR: ADR-0009 (ports-only module communication), ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package postgres

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

	"github.com/septagon-oss/pk-modules/pkg/migrate"
	"github.com/septagon-oss/pk-modules/pkg/user/store"
)

// Store is a database/sql-backed implementation of store.Store for Postgres.
// It is safe for concurrent use by multiple goroutines.
type Store struct {
	db *sql.DB
}

// New returns a Store wrapping the given *sql.DB and ensures the `users`
// table exists, so callers can pair a fresh database with the store without
// running the migration files explicitly.
func New(db *sql.DB) (*Store, error) {
	if db == nil {
		return nil, errors.New("user/postgres: nil *sql.DB")
	}
	s := &Store{db: db}
	if err := s.ensureSchema(context.Background()); err != nil {
		return nil, err
	}
	return s, nil
}

// Open opens a Postgres database with the given driverName and DSN and returns
// a Store. Caller is responsible for closing the returned *sql.DB via DB().
func Open(driverName, dsn string) (*Store, error) {
	db, err := sql.Open(driverName, dsn)
	if err != nil {
		return nil, fmt.Errorf("user/postgres: open: %w", err)
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
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS uq_users_tenant_email ON users(tenant_id, email);
CREATE UNIQUE INDEX IF NOT EXISTS uq_users_tenant_username ON users(tenant_id, username);
CREATE INDEX IF NOT EXISTS idx_users_tenant ON users(tenant_id);
`

// migrations is this adapter's schema history. 0001 is the schema exactly as
// it shipped before the ledger existed, so a database created by an earlier
// release adopts it as a recorded no-op rather than a rebuild. Never edit an
// applied migration — add the next one.
var migrations = []migrate.Migration{
	{Name: "0001_create_user", SQL: schemaDDL},
}

func (s *Store) ensureSchema(ctx context.Context) error {
	return migrate.Run(ctx, s.db, migrate.Options{Module: "user", Postgres: true}, migrations)
}

// Create inserts a user. Returns store.ErrDuplicateEmail or
// store.ErrDuplicateUsername if the tenant-scoped uniqueness is violated.
func (s *Store) Create(ctx context.Context, u *store.User) error {
	if u == nil {
		return errors.New("user/postgres: nil user")
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
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		u.ID, u.TenantID, u.Email, u.Username, u.PassHash, u.DisplayName, boolToInt(u.Active), u.CreatedAt, u.UpdatedAt,
	)
	if err != nil {
		return classifyUniqueError(err)
	}
	return nil
}

// Get returns a user by ID. The tenant predicate is mandatory so a leaked or
// guessed ID cannot read across tenants.
func (s *Store) Get(ctx context.Context, tenantID, id string) (*store.User, error) {
	row := s.db.QueryRowContext(ctx, selectColumns+` WHERE id = $1 AND tenant_id = $2`, id, tenantID)
	return scanUser(row)
}

// GetByEmail returns a user by (tenant_id, email).
func (s *Store) GetByEmail(ctx context.Context, tenantID, email string) (*store.User, error) {
	row := s.db.QueryRowContext(ctx, selectColumns+` WHERE tenant_id = $1 AND email = $2`, tenantID, email)
	return scanUser(row)
}

// GetByUsername returns a user by (tenant_id, username).
func (s *Store) GetByUsername(ctx context.Context, tenantID, username string) (*store.User, error) {
	row := s.db.QueryRowContext(ctx, selectColumns+` WHERE tenant_id = $1 AND username = $2`, tenantID, username)
	return scanUser(row)
}

// List returns users for a tenant, ordered by username. The tenant predicate is
// the first clause and is never optional.
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
		selectColumns+` WHERE tenant_id = $1 ORDER BY username LIMIT $2 OFFSET $3`,
		tenantID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("user/postgres: list: %w", err)
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
		return nil, fmt.Errorf("user/postgres: list rows: %w", err)
	}
	return out, nil
}

// Update overwrites a user's mutable fields (except pass_hash; use
// UpdatePassHash for credential changes). The WHERE clause matches on
// (id, tenant_id) and the SET list never touches tenant_id, so a row cannot be
// reassigned to another tenant.
func (s *Store) Update(ctx context.Context, u *store.User) error {
	if u == nil {
		return errors.New("user/postgres: nil user")
	}
	u.UpdatedAt = time.Now().UTC()
	res, err := s.db.ExecContext(
		ctx,
		`UPDATE users SET email = $1, username = $2, display_name = $3, active = $4, updated_at = $5 WHERE id = $6 AND tenant_id = $7`,
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
		`UPDATE users SET pass_hash = $1, updated_at = $2 WHERE id = $3 AND tenant_id = $4`,
		passHash, updatedAt, id, tenantID,
	)
	if err != nil {
		return fmt.Errorf("user/postgres: update pass_hash: %w", err)
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
	res, err := s.db.ExecContext(ctx, `DELETE FROM users WHERE id = $1 AND tenant_id = $2`, id, tenantID)
	if err != nil {
		return fmt.Errorf("user/postgres: delete: %w", err)
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
		return nil, fmt.Errorf("user/postgres: scan: %w", err)
	}
	return u, nil
}

func scanUserRows(row *sql.Rows) (*store.User, error) {
	u, err := scanInto(row)
	if err != nil {
		return nil, fmt.Errorf("user/postgres: scan rows: %w", err)
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

// classifyUniqueError maps a Postgres constraint error onto the store's
// sentinels. Postgres reports every unique conflict as SQLSTATE 23505
// (unique_violation) and names the offending constraint in the message —
// uq_users_tenant_email for the email index, uq_users_tenant_username for the
// username index, users_pkey for the primary key. We match on the message,
// mirroring the sqlite adapter: a recognized column maps to its specific
// sentinel, and anything unclassified returns the generic
// ErrUniqueConstraintViolation rather than guessing — guessing "duplicate
// email" on every unclassified conflict would hide real bugs by masking
// unrelated UNIQUE violations as user-facing email duplicates.
func classifyUniqueError(err error) error {
	if err == nil {
		return nil
	}
	msg := strings.ToLower(err.Error())
	if !strings.Contains(msg, "unique") && !strings.Contains(msg, "23505") {
		return fmt.Errorf("user/postgres: exec: %w", err)
	}
	// A duplicate primary key (users_pkey) is a duplicate id. The user store has
	// no dedicated duplicate-id sentinel, so — like the sqlite adapter, where an
	// id collision falls through to the generic case — report it as the generic
	// unique-constraint violation rather than misattributing it to email or
	// username. This branch also keeps a "users" table name from being read as a
	// "username" match below.
	if strings.Contains(msg, "_pkey") {
		return fmt.Errorf("%w: %v", store.ErrUniqueConstraintViolation, err)
	}
	switch {
	case strings.Contains(msg, "uq_users_tenant_email"):
		return store.ErrDuplicateEmail
	case strings.Contains(msg, "uq_users_tenant_username"):
		return store.ErrDuplicateUsername
	case strings.Contains(msg, "email"):
		return store.ErrDuplicateEmail
	case strings.Contains(msg, "username"):
		return store.ErrDuplicateUsername
	}
	return fmt.Errorf("%w: %v", store.ErrUniqueConstraintViolation, err)
}
