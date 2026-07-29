// Package sqlite is the default Store implementation for content_management
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

// Implements: REQ-CONTENT-001.
// Per: ADR-0017.
// Discipline: C-14.
import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/septagon-oss/pk-modules/pkg/content/store"
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

// New returns a Store wrapping the given *sql.DB and ensures the `content`
// table exists.
func New(db *sql.DB) (*Store, error) {
	if db == nil {
		return nil, errors.New("content/sqlite: nil *sql.DB")
	}
	s := &Store{db: db}
	if err := s.ensureSchema(context.Background()); err != nil {
		return nil, err
	}
	return s, nil
}

// Open opens a sqlite database with the given driverName and DSN and returns
// a Store.
func Open(driverName, dsn string) (*Store, error) {
	db, err := sql.Open(driverName, dsn)
	if err != nil {
		return nil, fmt.Errorf("content/sqlite: open: %w", err)
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
CREATE TABLE IF NOT EXISTS content (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    kind TEXT NOT NULL,
    slug TEXT NOT NULL,
    title TEXT NOT NULL,
    body TEXT NOT NULL,
    body_format TEXT NOT NULL,
    author_id TEXT NOT NULL,
    published_at DATETIME,
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL,
    UNIQUE(tenant_id, kind, slug)
);
CREATE INDEX IF NOT EXISTS idx_content_tenant ON content(tenant_id);
CREATE INDEX IF NOT EXISTS idx_content_published ON content(published_at);
`

// migrations is this adapter's schema history. 0001 is the schema exactly as
// it shipped before the ledger existed, so a database created by an earlier
// release adopts it as a recorded no-op rather than a rebuild. Never edit an
// applied migration — add the next one.
var migrations = []migrate.Migration{
	{Name: "0001_create_content", SQL: schemaDDL},
}

func (s *Store) ensureSchema(ctx context.Context) error {
	return migrate.Run(ctx, s.db, migrate.Options{Module: "content", Postgres: false}, migrations)
}

const selectColumns = `SELECT id, tenant_id, kind, slug, title, body, body_format, author_id, published_at, created_at, updated_at FROM content`

// Create inserts a content row. Returns store.ErrSlugTaken when the
// (tenant_id, kind, slug) tuple is already in use, or store.ErrDuplicate
// when the primary key collides.
func (s *Store) Create(ctx context.Context, c *store.Content) error {
	if c == nil {
		return errors.New("content/sqlite: nil content")
	}
	if c.CreatedAt.IsZero() {
		c.CreatedAt = time.Now().UTC()
	}
	if c.UpdatedAt.IsZero() {
		c.UpdatedAt = c.CreatedAt
	}
	_, err := s.db.ExecContext(
		ctx,
		`INSERT INTO content (id, tenant_id, kind, slug, title, body, body_format, author_id, published_at, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.ID, c.TenantID, c.Kind, c.Slug, c.Title, c.Body, c.BodyFormat, c.AuthorID,
		nullableTime(c.PublishedAt), c.CreatedAt, c.UpdatedAt,
	)
	if err != nil {
		return classifyInsertErr(err)
	}
	return nil
}

// Get returns a content row by (tenant_id, id). The tenant predicate is
// mandatory: a row owned by another tenant is reported as ErrNotFound, never
// returned, so a guessed or leaked ID cannot cross the tenant boundary.
func (s *Store) Get(ctx context.Context, tenantID, id string) (*store.Content, error) {
	row := s.db.QueryRowContext(ctx, selectColumns+` WHERE id = ? AND tenant_id = ?`, id, tenantID)
	c, err := scanRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, store.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("content/sqlite: scan: %w", err)
	}
	return c, nil
}

// GetBySlug returns a content row by (tenant_id, kind, slug).
func (s *Store) GetBySlug(ctx context.Context, tenantID, kind, slug string) (*store.Content, error) {
	row := s.db.QueryRowContext(
		ctx,
		selectColumns+` WHERE tenant_id = ? AND kind = ? AND slug = ?`,
		tenantID, kind, slug,
	)
	c, err := scanRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, store.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("content/sqlite: scan: %w", err)
	}
	return c, nil
}

// List returns content rows scoped to a tenant (and optionally a kind)
// ordered by created_at DESC, oldest entries trimmed by limit/offset.
func (s *Store) List(ctx context.Context, tenantID, kind string, limit, offset int) ([]*store.Content, error) {
	const defaultLimit = 100
	const maxLimit = 1000
	if limit <= 0 {
		limit = defaultLimit
	}
	if limit > maxLimit {
		limit = maxLimit
	}
	if offset < 0 {
		offset = 0
	}
	var (
		clauses = []string{"tenant_id = ?"}
		args    = []any{tenantID}
	)
	if kind != "" {
		clauses = append(clauses, "kind = ?")
		args = append(args, kind)
	}
	args = append(args, limit, offset)
	rows, err := s.db.QueryContext(
		ctx,
		selectColumns+` WHERE `+strings.Join(clauses, " AND ")+` ORDER BY created_at DESC LIMIT ? OFFSET ?`,
		args...,
	)
	if err != nil {
		return nil, fmt.Errorf("content/sqlite: list: %w", err)
	}
	defer rows.Close()
	var out []*store.Content
	for rows.Next() {
		c, err := scanRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("content/sqlite: list rows: %w", err)
	}
	return out, nil
}

// Update replaces the mutable fields of an existing row and bumps
// updated_at. created_at is preserved.
func (s *Store) Update(ctx context.Context, c *store.Content) error {
	if c == nil {
		return errors.New("content/sqlite: nil content")
	}
	c.UpdatedAt = time.Now().UTC()
	res, err := s.db.ExecContext(
		ctx,
		`UPDATE content SET kind = ?, slug = ?, title = ?, body = ?, body_format = ?, author_id = ?, updated_at = ?
		 WHERE id = ? AND tenant_id = ?`,
		c.Kind, c.Slug, c.Title, c.Body, c.BodyFormat, c.AuthorID, c.UpdatedAt, c.ID, c.TenantID,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return store.ErrSlugTaken
		}
		return fmt.Errorf("content/sqlite: update: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("content/postgres: rows affected: %w", err)
	}
	if rows == 0 {
		return store.ErrNotFound
	}
	return nil
}

// Delete removes a content row by (tenant_id, id). The tenant predicate is
// mandatory so a caller cannot delete another tenant's row by ID.
func (s *Store) Delete(ctx context.Context, tenantID, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM content WHERE id = ? AND tenant_id = ?`, id, tenantID)
	if err != nil {
		return fmt.Errorf("content/sqlite: delete: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("content/postgres: rows affected: %w", err)
	}
	if rows == 0 {
		return store.ErrNotFound
	}
	return nil
}

// SetPublished updates the published_at column for a row owned by tenantID.
// Passing a nil time clears it (transitioning the row back to draft). The
// tenant predicate is mandatory so a caller cannot publish another tenant's row.
func (s *Store) SetPublished(ctx context.Context, tenantID, id string, at *time.Time) error {
	now := time.Now().UTC()
	res, err := s.db.ExecContext(
		ctx,
		`UPDATE content SET published_at = ?, updated_at = ? WHERE id = ? AND tenant_id = ?`,
		nullableTime(at), now, id, tenantID,
	)
	if err != nil {
		return fmt.Errorf("content/sqlite: set published: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("content/postgres: rows affected: %w", err)
	}
	if rows == 0 {
		return store.ErrNotFound
	}
	return nil
}

type scannable interface {
	Scan(dest ...any) error
}

func scanRow(row *sql.Row) (*store.Content, error)   { return scanInto(row) }
func scanRows(row *sql.Rows) (*store.Content, error) { return scanInto(row) }

func scanInto(row scannable) (*store.Content, error) {
	c := &store.Content{}
	var published sql.NullTime
	if err := row.Scan(
		&c.ID, &c.TenantID, &c.Kind, &c.Slug, &c.Title, &c.Body, &c.BodyFormat, &c.AuthorID,
		&published, &c.CreatedAt, &c.UpdatedAt,
	); err != nil {
		return nil, err
	}
	if published.Valid {
		t := published.Time
		c.PublishedAt = &t
	}
	return c, nil
}

func nullableTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return *t
}

func classifyInsertErr(err error) error {
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "tenant_id") && strings.Contains(msg, "unique"):
		return store.ErrSlugTaken
	case strings.Contains(msg, "primary key"):
		return store.ErrDuplicate
	case strings.Contains(msg, "unique"):
		return store.ErrSlugTaken
	}
	return fmt.Errorf("content/sqlite: insert: %w", err)
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
