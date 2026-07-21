// Package sqlite is the default Store implementation for api_key_management
// backed by database/sql.
//
// sqlite.go owns the Store implementation. Callers must register their
// preferred sqlite driver (typically `_ "modernc.org/sqlite"`) before
// opening a database; this package does not import any driver so binaries
// stay slim and the driver is a build-time concern of the host application.
//
// ADR: ADR-0009 (ports-only module communication), ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/septagon-oss/pk-modules/pkg/apikey/store"
)

// Store is a database/sql-backed implementation of store.Store. It is safe
// for concurrent use by multiple goroutines.
type Store struct {
	db *sql.DB
}

// New returns a Store wrapping the given *sql.DB and ensures the
// `api_keys` table exists.
func New(db *sql.DB) (*Store, error) {
	if db == nil {
		return nil, errors.New("apikey/sqlite: nil *sql.DB")
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
		return nil, fmt.Errorf("apikey/sqlite: open: %w", err)
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
CREATE TABLE IF NOT EXISTS api_keys (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    user_id TEXT NOT NULL,
    name TEXT NOT NULL,
    prefix TEXT NOT NULL,
    hash TEXT NOT NULL,
    scopes TEXT NOT NULL,
    last_used_at DATETIME,
    revoked_at DATETIME,
    expires_at DATETIME,
    created_at DATETIME NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_api_keys_prefix ON api_keys(prefix);
CREATE INDEX IF NOT EXISTS idx_api_keys_tenant ON api_keys(tenant_id);
`

func (s *Store) ensureSchema(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, schemaDDL); err != nil {
		return fmt.Errorf("apikey/sqlite: ensure schema: %w", err)
	}
	return nil
}

// Create inserts an API key. Returns store.ErrDuplicate on PK collision.
func (s *Store) Create(ctx context.Context, k *store.APIKey) error {
	if k == nil {
		return errors.New("apikey/sqlite: nil key")
	}
	if k.CreatedAt.IsZero() {
		k.CreatedAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(
		ctx,
		`INSERT INTO api_keys (id, tenant_id, user_id, name, prefix, hash, scopes, last_used_at, revoked_at, expires_at, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		k.ID, k.TenantID, k.UserID, k.Name, k.Prefix, k.Hash, k.Scopes,
		nullableTime(k.LastUsedAt), nullableTime(k.RevokedAt), nullableTime(k.ExpiresAt), k.CreatedAt,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return store.ErrDuplicate
		}
		return fmt.Errorf("apikey/sqlite: insert: %w", err)
	}
	return nil
}

// Get returns the API key identified by (tenantID, id). The tenant predicate
// is mandatory so a management caller cannot read another tenant's key by ID.
// (Authentication uses GetByPrefix, which is intentionally global because the
// presented key itself selects the tenant.)
func (s *Store) Get(ctx context.Context, tenantID, id string) (*store.APIKey, error) {
	row := s.db.QueryRowContext(ctx, selectColumns+` WHERE id = ? AND tenant_id = ?`, id, tenantID)
	k, err := scanKey(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, store.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("apikey/sqlite: scan: %w", err)
	}
	return k, nil
}

// GetByPrefix returns every (non-revoked or revoked) key sharing the same
// prefix. The caller is responsible for verifying the bcrypt hash before
// trusting any candidate.
func (s *Store) GetByPrefix(ctx context.Context, prefix string) ([]*store.APIKey, error) {
	rows, err := s.db.QueryContext(ctx, selectColumns+` WHERE prefix = ?`, prefix)
	if err != nil {
		return nil, fmt.Errorf("apikey/sqlite: query by prefix: %w", err)
	}
	defer rows.Close()
	var out []*store.APIKey
	for rows.Next() {
		k, err := scanKeyRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("apikey/sqlite: prefix rows: %w", err)
	}
	return out, nil
}

// List returns every API key for the tenant ordered by created_at desc.
func (s *Store) List(ctx context.Context, tenantID string) ([]*store.APIKey, error) {
	rows, err := s.db.QueryContext(
		ctx,
		selectColumns+` WHERE tenant_id = ? ORDER BY created_at DESC`,
		tenantID,
	)
	if err != nil {
		return nil, fmt.Errorf("apikey/sqlite: list: %w", err)
	}
	defer rows.Close()
	var out []*store.APIKey
	for rows.Next() {
		k, err := scanKeyRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("apikey/sqlite: list rows: %w", err)
	}
	return out, nil
}

// Revoke marks the key identified by (tenantID, id) revoked at time.Now().UTC().
// The tenant predicate is mandatory so a caller cannot revoke another tenant's
// key by ID.
func (s *Store) Revoke(ctx context.Context, tenantID, id string) error {
	now := time.Now().UTC()
	res, err := s.db.ExecContext(
		ctx,
		`UPDATE api_keys SET revoked_at = ? WHERE id = ? AND tenant_id = ? AND revoked_at IS NULL`,
		now, id, tenantID,
	)
	if err != nil {
		return fmt.Errorf("apikey/sqlite: revoke: %w", err)
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		if _, getErr := s.Get(ctx, tenantID, id); errors.Is(getErr, store.ErrNotFound) {
			return store.ErrNotFound
		}
	}
	return nil
}

// UpdateLastUsed bumps the last_used_at timestamp for the key identified by
// (tenantID, id). During authentication the caller passes the tenant of the
// key it just verified, so this stays tenant-scoped end to end.
func (s *Store) UpdateLastUsed(ctx context.Context, tenantID, id string, at time.Time) error {
	if at.IsZero() {
		at = time.Now().UTC()
	}
	_, err := s.db.ExecContext(
		ctx,
		`UPDATE api_keys SET last_used_at = ? WHERE id = ? AND tenant_id = ?`,
		at, id, tenantID,
	)
	if err != nil {
		return fmt.Errorf("apikey/sqlite: update last_used_at: %w", err)
	}
	return nil
}

const selectColumns = `SELECT id, tenant_id, user_id, name, prefix, hash, scopes, last_used_at, revoked_at, expires_at, created_at FROM api_keys`

type scannable interface {
	Scan(dest ...any) error
}

func scanKey(row *sql.Row) (*store.APIKey, error) {
	return scanInto(row)
}

func scanKeyRows(row *sql.Rows) (*store.APIKey, error) {
	k, err := scanInto(row)
	if err != nil {
		return nil, fmt.Errorf("apikey/sqlite: scan rows: %w", err)
	}
	return k, nil
}

func scanInto(row scannable) (*store.APIKey, error) {
	k := &store.APIKey{}
	var lastUsed, revoked, expires sql.NullTime
	if err := row.Scan(
		&k.ID, &k.TenantID, &k.UserID, &k.Name, &k.Prefix, &k.Hash, &k.Scopes,
		&lastUsed, &revoked, &expires, &k.CreatedAt,
	); err != nil {
		return nil, err
	}
	if lastUsed.Valid {
		t := lastUsed.Time
		k.LastUsedAt = &t
	}
	if revoked.Valid {
		t := revoked.Time
		k.RevokedAt = &t
	}
	if expires.Valid {
		t := expires.Time
		k.ExpiresAt = &t
	}
	return k, nil
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
	return strings.Contains(msg, "unique") || strings.Contains(msg, "constraint")
}
