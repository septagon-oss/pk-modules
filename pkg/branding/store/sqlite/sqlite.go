// Package sqlite is the default Store implementation for branding_management
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

// Implements: REQ-BRANDING-001.
// Per: ADR-0017.
// Discipline: C-14.
import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/septagon-oss/pk-modules/pkg/branding/store"
	"github.com/septagon-oss/pk-modules/pkg/migrate"
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
// `branding_profiles` table exists, so callers can pair a fresh in-memory
// database with the store without running the migration files explicitly.
func New(db *sql.DB) (*Store, error) {
	if db == nil {
		return nil, errors.New("branding/sqlite: nil *sql.DB")
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
		return nil, fmt.Errorf("branding/sqlite: open: %w", err)
	}
	s, err := New(db)
	if err != nil {
		// justified: constructor failure path; the close error is non-actionable.
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// DB exposes the underlying *sql.DB so the caller controls lifecycle.
func (s *Store) DB() *sql.DB { return s.db }

const schemaDDL = `
CREATE TABLE IF NOT EXISTS branding_profiles (
    tenant_id          TEXT PRIMARY KEY,
    display_name       TEXT NOT NULL,
    logo_data          BLOB,
    logo_content_type  TEXT NOT NULL DEFAULT '',
    logo_alt           TEXT NOT NULL DEFAULT '',
    primary_color      TEXT NOT NULL DEFAULT '',
    font_key           TEXT NOT NULL DEFAULT '',
    setup_completed_at DATETIME,
    created_at         DATETIME NOT NULL,
    updated_at         DATETIME NOT NULL
);
`

// migrations is this adapter's schema history. 0001 is the schema exactly as
// it shipped before the ledger existed, so a database created by an earlier
// release adopts it as a recorded no-op rather than a rebuild. Never edit an
// applied migration — add the next one.
var migrations = []migrate.Migration{
	{Name: "0001_create_branding", SQL: schemaDDL},
}

func (s *Store) ensureSchema(ctx context.Context) error {
	return migrate.Run(ctx, s.db, migrate.Options{Module: "branding", Postgres: false}, migrations)
}

const selectColumns = `SELECT tenant_id, display_name, logo_data, logo_content_type, logo_alt,
	primary_color, font_key, setup_completed_at, created_at, updated_at FROM branding_profiles`

// Get returns the branding record for tenantID, or store.ErrNotFound if none
// exists yet.
func (s *Store) Get(ctx context.Context, tenantID string) (*store.Record, error) {
	row := s.db.QueryRowContext(ctx, selectColumns+` WHERE tenant_id = ?`, tenantID)
	r, err := scanRecord(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, store.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("branding/sqlite: scan: %w", err)
	}
	return r, nil
}

// Upsert creates or replaces the branding record for r.TenantID. Timestamps
// are assigned here, not by the caller: CreatedAt is stamped when the caller
// left it zero, but only takes effect on the first insert for a tenant — the
// ON CONFLICT branch below omits created_at from its SET list, so an existing
// row's original value is always preserved. UpdatedAt always advances to now.
func (s *Store) Upsert(ctx context.Context, r *store.Record) error {
	if r == nil {
		return errors.New("branding/sqlite: nil record")
	}
	now := time.Now().UTC()
	if r.CreatedAt.IsZero() {
		r.CreatedAt = now
	}
	r.UpdatedAt = now
	_, err := s.db.ExecContext(
		ctx,
		`INSERT INTO branding_profiles (
			tenant_id, display_name, logo_data, logo_content_type, logo_alt,
			primary_color, font_key, setup_completed_at, created_at, updated_at
		 ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(tenant_id) DO UPDATE SET
			display_name = excluded.display_name,
			logo_data = excluded.logo_data,
			logo_content_type = excluded.logo_content_type,
			logo_alt = excluded.logo_alt,
			primary_color = excluded.primary_color,
			font_key = excluded.font_key,
			setup_completed_at = excluded.setup_completed_at,
			updated_at = excluded.updated_at`,
		r.TenantID, r.DisplayName, nullableBytes(r.LogoData), r.LogoContentType, r.LogoAlt,
		r.PrimaryColor, r.FontKey, nullableTime(r.SetupCompletedAt), r.CreatedAt, r.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("branding/sqlite: upsert: %w", err)
	}
	return nil
}

func scanRecord(row *sql.Row) (*store.Record, error) {
	r := &store.Record{}
	var logoData []byte
	var setupCompletedAt sql.NullTime
	if err := row.Scan(
		&r.TenantID, &r.DisplayName, &logoData, &r.LogoContentType, &r.LogoAlt,
		&r.PrimaryColor, &r.FontKey, &setupCompletedAt, &r.CreatedAt, &r.UpdatedAt,
	); err != nil {
		return nil, err
	}
	r.LogoData = logoData
	if setupCompletedAt.Valid {
		t := setupCompletedAt.Time
		r.SetupCompletedAt = &t
	}
	return r, nil
}

// nullableBytes maps an empty or nil logo payload to SQL NULL so "no logo" is
// unambiguous on read, rather than relying on driver-specific handling of a
// nil []byte parameter.
func nullableBytes(b []byte) any {
	if len(b) == 0 {
		return nil
	}
	return b
}

func nullableTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return *t
}
