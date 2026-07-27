// Package postgres is the Postgres Store implementation for api_key_management
// backed by database/sql.
//
// postgres.go is the production sibling of store/sqlite: the same Store
// contract, the same mandatory tenant predicate on every management query, the
// same intentionally-global authentication lookup, and the same conformance
// suite — only the SQL dialect differs ($N placeholders, TIMESTAMPTZ columns,
// SQLSTATE-based error classification). Callers register a Postgres driver
// (typically `_ "github.com/jackc/pgx/v5/stdlib"`, driver name "pgx") before
// opening a database; this package imports no driver so the binary stays slim
// and the driver is the host application's build-time choice.
//
// ADR: ADR-0009 (ports-only module communication), ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package postgres

// Implements: REQ-APIKEY-001.
// Per: ADR-0017.
// Discipline: C-14.
import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/septagon-oss/pk-modules/pkg/apikey/store"
	"github.com/septagon-oss/pk-modules/pkg/migrate"
)

// Store is a database/sql-backed implementation of store.Store for Postgres.
// It is safe for concurrent use by multiple goroutines.
type Store struct {
	db *sql.DB
}

// New returns a Store wrapping the given *sql.DB and ensures the `api_keys`
// table exists.
func New(db *sql.DB) (*Store, error) {
	if db == nil {
		return nil, errors.New("apikey/postgres: nil *sql.DB")
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
		return nil, fmt.Errorf("apikey/postgres: open: %w", err)
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
    last_used_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ,
    expires_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_api_keys_prefix ON api_keys(prefix);
CREATE INDEX IF NOT EXISTS idx_api_keys_tenant ON api_keys(tenant_id);
`

// migrations is this adapter's schema history. 0001 is the schema exactly as
// it shipped before the ledger existed, so a database created by an earlier
// release adopts it as a recorded no-op rather than a rebuild. Never edit an
// applied migration — add the next one.
var migrations = []migrate.Migration{
	{Name: "0001_create_apikey", SQL: schemaDDL},
}

func (s *Store) ensureSchema(ctx context.Context) error {
	return migrate.Run(ctx, s.db, migrate.Options{Module: "apikey", Postgres: true}, migrations)
}

// Create inserts an API key. Returns store.ErrDuplicate on PK collision.
func (s *Store) Create(ctx context.Context, k *store.APIKey) error {
	if k == nil {
		return errors.New("apikey/postgres: nil key")
	}
	if k.CreatedAt.IsZero() {
		k.CreatedAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(
		ctx,
		`INSERT INTO api_keys (id, tenant_id, user_id, name, prefix, hash, scopes, last_used_at, revoked_at, expires_at, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		k.ID, k.TenantID, k.UserID, k.Name, k.Prefix, k.Hash, k.Scopes,
		nullableTime(k.LastUsedAt), nullableTime(k.RevokedAt), nullableTime(k.ExpiresAt), k.CreatedAt,
	)
	if err != nil {
		return classifyInsertErr(err)
	}
	return nil
}

// Get returns the API key identified by (tenantID, id). The tenant predicate
// is mandatory so a management caller cannot read another tenant's key by ID.
// (Authentication uses GetByPrefix, which is intentionally global because the
// presented key itself selects the tenant.)
func (s *Store) Get(ctx context.Context, tenantID, id string) (*store.APIKey, error) {
	row := s.db.QueryRowContext(ctx, selectColumns+` WHERE id = $1 AND tenant_id = $2`, id, tenantID)
	k, err := scanKey(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, store.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("apikey/postgres: scan: %w", err)
	}
	return k, nil
}

// GetByPrefix returns every (non-revoked or revoked) key sharing the same
// prefix. The caller is responsible for verifying the bcrypt hash before
// trusting any candidate.
//
// This lookup carries NO tenant predicate by design: it is the authentication
// path, where the presented key selects its own tenant. A management caller
// never reaches a foreign key this way — it holds only the prefix of a key it
// was handed — and the row's own tenant_id is what the caller trusts once the
// hash verifies. The mandatory tenant scoping lives on the by-ID surface (Get,
// Revoke, UpdateLastUsed) instead.
func (s *Store) GetByPrefix(ctx context.Context, prefix string) ([]*store.APIKey, error) {
	rows, err := s.db.QueryContext(ctx, selectColumns+` WHERE prefix = $1`, prefix)
	if err != nil {
		return nil, fmt.Errorf("apikey/postgres: query by prefix: %w", err)
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
		return nil, fmt.Errorf("apikey/postgres: prefix rows: %w", err)
	}
	return out, nil
}

// List returns the tenant's active API keys ordered by created_at desc.
// Revoked keys are excluded: revocation is the delete operation exposed to
// operators, and a revoked key resurfacing in the list reads as a failed
// delete. Revocations remain visible through the audit trail. The tenant
// predicate is the first clause and is never optional.
func (s *Store) List(ctx context.Context, tenantID string) ([]*store.APIKey, error) {
	rows, err := s.db.QueryContext(
		ctx,
		selectColumns+` WHERE tenant_id = $1 AND revoked_at IS NULL ORDER BY created_at DESC`,
		tenantID,
	)
	if err != nil {
		return nil, fmt.Errorf("apikey/postgres: list: %w", err)
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
		return nil, fmt.Errorf("apikey/postgres: list rows: %w", err)
	}
	return out, nil
}

// Revoke marks the key identified by (tenantID, id) revoked at time.Now().UTC().
// The tenant predicate is mandatory so a caller cannot revoke another tenant's
// key by ID. The revoked_at IS NULL guard makes a second revoke a no-op, and a
// zero-row result is disambiguated by a follow-up Get: missing rows report
// ErrNotFound, already-revoked rows succeed silently.
func (s *Store) Revoke(ctx context.Context, tenantID, id string) error {
	now := time.Now().UTC()
	res, err := s.db.ExecContext(
		ctx,
		`UPDATE api_keys SET revoked_at = $1 WHERE id = $2 AND tenant_id = $3 AND revoked_at IS NULL`,
		now, id, tenantID,
	)
	if err != nil {
		return fmt.Errorf("apikey/postgres: revoke: %w", err)
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
		`UPDATE api_keys SET last_used_at = $1 WHERE id = $2 AND tenant_id = $3`,
		at, id, tenantID,
	)
	if err != nil {
		return fmt.Errorf("apikey/postgres: update last_used_at: %w", err)
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
		return nil, fmt.Errorf("apikey/postgres: scan rows: %w", err)
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

// classifyInsertErr maps a Postgres insert error onto the store's sentinels.
// api_keys has exactly one unique constraint — the primary key on id, which
// Postgres names api_keys_pkey — so every SQLSTATE 23505 (unique_violation) on
// this table is a duplicate ID and maps to store.ErrDuplicate. That is the
// sqlite adapter's mapping exactly (any UNIQUE violation -> ErrDuplicate); there
// is no second unique index to distinguish, unlike content's (tenant, kind,
// slug) index. The pkey name is matched explicitly so a future unique
// constraint added to this table would fall through to a wrapped error rather
// than be silently mislabeled a duplicate ID.
func classifyInsertErr(err error) error {
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "api_keys_pkey") {
		return store.ErrDuplicate
	}
	if isUniqueViolation(err) {
		return store.ErrDuplicate
	}
	return fmt.Errorf("apikey/postgres: insert: %w", err)
}

// isUniqueViolation reports whether err is a Postgres unique-constraint
// violation. pgx renders SQLSTATE 23505 with the text "duplicate key value
// violates unique constraint", so matching "unique" is precise here and does
// not catch NOT NULL / CHECK / FK failures. The bare SQLSTATE code is matched
// too for robustness across driver error renderings.
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique") || strings.Contains(msg, "23505")
}
