// Package postgres is the Postgres Store implementation for tenant_management
// backed by database/sql.
//
// postgres.go is the production sibling of store/sqlite: the same Store
// contract, the same lookups keyed on a globally-unique id or slug — a tenant is
// itself the isolation boundary, so there is no tenant_id column to scope by —
// and the same slug-uniqueness guarantee. Only the SQL dialect differs ($N
// placeholders, TIMESTAMPTZ columns, SQLSTATE-based error classification).
// Callers register a Postgres driver (typically `_ "github.com/jackc/pgx/v5/stdlib"`,
// driver name "pgx") before opening a database; this package imports no driver so
// the binary stays slim and the driver is the host application's build-time choice.
//
// ADR: ADR-0009 (ports-only module communication), ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package postgres

// Implements: REQ-TENANT-001.
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
	"github.com/septagon-oss/pk-modules/pkg/tenant/store"
)

// Store is a database/sql-backed implementation of store.Store for Postgres.
// It is safe for concurrent use by multiple goroutines.
type Store struct {
	db *sql.DB
}

// New returns a Store wrapping the given *sql.DB and ensures the `tenants`
// table exists, so callers can pair a fresh database with the store without
// running the migration files explicitly.
func New(db *sql.DB) (*Store, error) {
	if db == nil {
		return nil, errors.New("tenant/postgres: nil *sql.DB")
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
		return nil, fmt.Errorf("tenant/postgres: open: %w", err)
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
CREATE TABLE IF NOT EXISTS tenants (
    id TEXT PRIMARY KEY,
    slug TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_tenants_slug ON tenants(slug);
`

// migrations is this adapter's schema history. 0001 is the schema exactly as
// it shipped before the ledger existed, so a database created by an earlier
// release adopts it as a recorded no-op rather than a rebuild. Never edit an
// applied migration — add the next one.
var migrations = []migrate.Migration{
	{Name: "0001_create_tenant", SQL: schemaDDL},
}

func (s *Store) ensureSchema(ctx context.Context) error {
	return migrate.Run(ctx, s.db, migrate.Options{Module: "tenant", Postgres: true}, migrations)
}

// Create inserts a tenant. Returns store.ErrDuplicateSlug when the slug is
// already in use.
func (s *Store) Create(ctx context.Context, t *store.Tenant) error {
	if t == nil {
		return errors.New("tenant/postgres: nil tenant")
	}
	now := time.Now().UTC()
	if t.CreatedAt.IsZero() {
		t.CreatedAt = now
	}
	if t.UpdatedAt.IsZero() {
		t.UpdatedAt = now
	}
	_, err := s.db.ExecContext(
		ctx,
		`INSERT INTO tenants (id, slug, name, created_at, updated_at) VALUES ($1, $2, $3, $4, $5)`,
		t.ID, t.Slug, t.Name, t.CreatedAt, t.UpdatedAt,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return store.ErrDuplicateSlug
		}
		return fmt.Errorf("tenant/postgres: create: %w", err)
	}
	return nil
}

// Get returns a tenant by ID. A tenant is the top-level isolation entity, so
// the lookup is by globally-unique primary key with no tenant predicate — there
// is no tenant_id column to scope by.
func (s *Store) Get(ctx context.Context, id string) (*store.Tenant, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, slug, name, created_at, updated_at FROM tenants WHERE id = $1`, id)
	return scanTenant(row)
}

// GetBySlug returns a tenant by slug. The slug is globally unique, so the
// lookup carries no tenant predicate.
func (s *Store) GetBySlug(ctx context.Context, slug string) (*store.Tenant, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, slug, name, created_at, updated_at FROM tenants WHERE slug = $1`, slug)
	return scanTenant(row)
}

// List returns every tenant ordered by slug. There is no tenant predicate: the
// tenant registry is the global list of tenants itself.
func (s *Store) List(ctx context.Context) ([]*store.Tenant, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, slug, name, created_at, updated_at FROM tenants ORDER BY slug`)
	if err != nil {
		return nil, fmt.Errorf("tenant/postgres: list: %w", err)
	}
	defer rows.Close()
	var out []*store.Tenant
	for rows.Next() {
		t := &store.Tenant{}
		if err := rows.Scan(&t.ID, &t.Slug, &t.Name, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, fmt.Errorf("tenant/postgres: list scan: %w", err)
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("tenant/postgres: list rows: %w", err)
	}
	return out, nil
}

// Update overwrites a tenant's mutable fields, matching on the primary key.
// Returns store.ErrNotFound when no tenant matches and store.ErrDuplicateSlug on
// a slug conflict.
func (s *Store) Update(ctx context.Context, t *store.Tenant) error {
	if t == nil {
		return errors.New("tenant/postgres: nil tenant")
	}
	t.UpdatedAt = time.Now().UTC()
	res, err := s.db.ExecContext(
		ctx,
		`UPDATE tenants SET slug = $1, name = $2, updated_at = $3 WHERE id = $4`,
		t.Slug, t.Name, t.UpdatedAt, t.ID,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return store.ErrDuplicateSlug
		}
		return fmt.Errorf("tenant/postgres: update: %w", err)
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return store.ErrNotFound
	}
	return nil
}

// Delete removes a tenant by ID, returning store.ErrNotFound when no tenant
// matches. The lookup is by globally-unique primary key with no tenant predicate.
func (s *Store) Delete(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM tenants WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("tenant/postgres: delete: %w", err)
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return store.ErrNotFound
	}
	return nil
}

func scanTenant(row *sql.Row) (*store.Tenant, error) {
	t := &store.Tenant{}
	err := row.Scan(&t.ID, &t.Slug, &t.Name, &t.CreatedAt, &t.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, store.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("tenant/postgres: scan: %w", err)
	}
	return t, nil
}

// isUniqueViolation reports whether err is a Postgres unique-constraint
// violation. pgx renders SQLSTATE 23505 with the text "duplicate key value
// violates unique constraint <name>", so matching "unique" is precise here and
// does not catch NOT NULL / CHECK / FK failures.
//
// Both a duplicate primary key (constraint tenants_pkey) and a duplicate slug
// (constraint tenants_slug_key) surface as 23505. Unlike content_management, the
// tenant store exposes a single duplicate sentinel — there is no ErrDuplicate for
// an id collision — so, exactly as the sqlite adapter's isUniqueConstraint check
// does, every unique violation (pkey or slug_key) collapses to ErrDuplicateSlug
// at the call site. The `_pkey` suffix is therefore not split out: there is no
// distinct sentinel to map it to.
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique") || strings.Contains(msg, "23505")
}
