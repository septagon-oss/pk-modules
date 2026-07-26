// Package sqlite is the default Store implementation backed by
// database/sql.
//
// sqlite.go owns the Store implementation. Callers must register their
// preferred sqlite driver (typically `_ "modernc.org/sqlite"`) before opening
// a database; this package does not import any driver so binaries stay slim
// and the driver is a build-time concern of the host application.
//
// ADR: ADR-0009 (ports-only module communication), ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package sqlite

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

	"github.com/septagon-oss/pk-modules/pkg/tenant/store"
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
// `tenants` table exists, so callers can pair a fresh in-memory database with
// the store without running the migration files explicitly.
func New(db *sql.DB) (*Store, error) {
	if db == nil {
		return nil, errors.New("tenant/sqlite: nil *sql.DB")
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
		return nil, fmt.Errorf("tenant/sqlite: open: %w", err)
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
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_tenants_slug ON tenants(slug);
`

func (s *Store) ensureSchema(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, schemaDDL); err != nil {
		return fmt.Errorf("tenant/sqlite: ensure schema: %w", err)
	}
	return nil
}

// Create inserts a tenant. Returns store.ErrDuplicateSlug when the slug is
// already in use.
func (s *Store) Create(ctx context.Context, t *store.Tenant) error {
	if t == nil {
		return errors.New("tenant/sqlite: nil tenant")
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
		`INSERT INTO tenants (id, slug, name, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		t.ID, t.Slug, t.Name, t.CreatedAt, t.UpdatedAt,
	)
	if err != nil {
		if isUniqueConstraint(err) {
			return store.ErrDuplicateSlug
		}
		return fmt.Errorf("tenant/sqlite: create: %w", err)
	}
	return nil
}

// Get returns a tenant by ID.
func (s *Store) Get(ctx context.Context, id string) (*store.Tenant, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, slug, name, created_at, updated_at FROM tenants WHERE id = ?`, id)
	return scanTenant(row)
}

// GetBySlug returns a tenant by slug.
func (s *Store) GetBySlug(ctx context.Context, slug string) (*store.Tenant, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, slug, name, created_at, updated_at FROM tenants WHERE slug = ?`, slug)
	return scanTenant(row)
}

// List returns every tenant ordered by slug.
func (s *Store) List(ctx context.Context) ([]*store.Tenant, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, slug, name, created_at, updated_at FROM tenants ORDER BY slug`)
	if err != nil {
		return nil, fmt.Errorf("tenant/sqlite: list: %w", err)
	}
	defer rows.Close()
	var out []*store.Tenant
	for rows.Next() {
		t := &store.Tenant{}
		if err := rows.Scan(&t.ID, &t.Slug, &t.Name, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, fmt.Errorf("tenant/sqlite: list scan: %w", err)
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("tenant/sqlite: list rows: %w", err)
	}
	return out, nil
}

// Update overwrites a tenant's mutable fields.
func (s *Store) Update(ctx context.Context, t *store.Tenant) error {
	if t == nil {
		return errors.New("tenant/sqlite: nil tenant")
	}
	t.UpdatedAt = time.Now().UTC()
	res, err := s.db.ExecContext(
		ctx,
		`UPDATE tenants SET slug = ?, name = ?, updated_at = ? WHERE id = ?`,
		t.Slug, t.Name, t.UpdatedAt, t.ID,
	)
	if err != nil {
		if isUniqueConstraint(err) {
			return store.ErrDuplicateSlug
		}
		return fmt.Errorf("tenant/sqlite: update: %w", err)
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return store.ErrNotFound
	}
	return nil
}

// Delete removes a tenant by ID.
func (s *Store) Delete(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM tenants WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("tenant/sqlite: delete: %w", err)
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
		return nil, fmt.Errorf("tenant/sqlite: scan: %w", err)
	}
	return t, nil
}

// isUniqueConstraint inspects an error returned by sqlite drivers and reports
// whether it is a UNIQUE constraint violation. We match on the error message
// because modernc.org/sqlite returns errors whose exposed type lives in an
// internal package — string matching keeps this package driver-agnostic.
func isUniqueConstraint(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique constraint") ||
		strings.Contains(msg, "constraint failed: unique") ||
		strings.Contains(msg, "constraint failed: tenants.slug")
}
