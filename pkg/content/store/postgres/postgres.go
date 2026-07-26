// Package postgres is the Postgres Store implementation for content_management
// backed by database/sql.
//
// postgres.go is the production sibling of store/sqlite: the same Store
// contract, the same mandatory tenant predicate on every query, the same
// conformance suite — only the SQL dialect differs ($N placeholders, TIMESTAMPTZ
// columns, SQLSTATE-based error classification). Callers register a Postgres
// driver (typically `_ "github.com/jackc/pgx/v5/stdlib"`, driver name "pgx")
// before opening a database; this package imports no driver so the binary stays
// slim and the driver is the host application's build-time choice.
//
// ADR: ADR-0009 (ports-only module communication), ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package postgres

// Implements: REQ-CONTENT-001.
// Per: ADR-0017.
// Discipline: C-14.
import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/septagon-oss/pk-modules/pkg/content/store"
)

// Store is a database/sql-backed implementation of store.Store for Postgres.
// It is safe for concurrent use by multiple goroutines.
type Store struct {
	db *sql.DB
}

// New returns a Store wrapping the given *sql.DB and ensures the `content`
// table exists.
func New(db *sql.DB) (*Store, error) {
	if db == nil {
		return nil, errors.New("content/postgres: nil *sql.DB")
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
		return nil, fmt.Errorf("content/postgres: open: %w", err)
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
CREATE TABLE IF NOT EXISTS content (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    kind TEXT NOT NULL,
    slug TEXT NOT NULL,
    title TEXT NOT NULL,
    body TEXT NOT NULL,
    body_format TEXT NOT NULL,
    author_id TEXT NOT NULL,
    published_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    UNIQUE(tenant_id, kind, slug)
);
CREATE INDEX IF NOT EXISTS idx_content_tenant ON content(tenant_id);
CREATE INDEX IF NOT EXISTS idx_content_published ON content(published_at);
`

func (s *Store) ensureSchema(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, schemaDDL); err != nil {
		return fmt.Errorf("content/postgres: ensure schema: %w", err)
	}
	return nil
}

const selectColumns = `SELECT id, tenant_id, kind, slug, title, body, body_format, author_id, published_at, created_at, updated_at FROM content`

// Create inserts a content row. Returns store.ErrSlugTaken when the
// (tenant_id, kind, slug) tuple is already in use, or store.ErrDuplicate when
// the primary key collides.
func (s *Store) Create(ctx context.Context, c *store.Content) error {
	if c == nil {
		return errors.New("content/postgres: nil content")
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
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
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
	row := s.db.QueryRowContext(ctx, selectColumns+` WHERE id = $1 AND tenant_id = $2`, id, tenantID)
	c, err := scanRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, store.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("content/postgres: scan: %w", err)
	}
	return c, nil
}

// GetBySlug returns a content row by (tenant_id, kind, slug).
func (s *Store) GetBySlug(ctx context.Context, tenantID, kind, slug string) (*store.Content, error) {
	row := s.db.QueryRowContext(
		ctx,
		selectColumns+` WHERE tenant_id = $1 AND kind = $2 AND slug = $3`,
		tenantID, kind, slug,
	)
	c, err := scanRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, store.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("content/postgres: scan: %w", err)
	}
	return c, nil
}

// List returns content rows scoped to a tenant (and optionally a kind) ordered
// by created_at DESC, paged by limit/offset. The tenant predicate is the first
// clause and is never optional.
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
		clauses = []string{"tenant_id = $1"}
		args    = []any{tenantID}
	)
	next := 2
	if kind != "" {
		clauses = append(clauses, "kind = $"+strconv.Itoa(next))
		args = append(args, kind)
		next++
	}
	limitPlaceholder := "$" + strconv.Itoa(next)
	offsetPlaceholder := "$" + strconv.Itoa(next+1)
	args = append(args, limit, offset)
	rows, err := s.db.QueryContext(
		ctx,
		selectColumns+` WHERE `+strings.Join(clauses, " AND ")+
			` ORDER BY created_at DESC LIMIT `+limitPlaceholder+` OFFSET `+offsetPlaceholder,
		args...,
	)
	if err != nil {
		return nil, fmt.Errorf("content/postgres: list: %w", err)
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
		return nil, fmt.Errorf("content/postgres: list rows: %w", err)
	}
	return out, nil
}

// Update replaces the mutable fields of an existing row and bumps updated_at.
// created_at and tenant_id are preserved: the WHERE clause matches on
// (id, tenant_id) and the SET list never touches tenant_id, so a row cannot be
// reassigned to another tenant.
func (s *Store) Update(ctx context.Context, c *store.Content) error {
	if c == nil {
		return errors.New("content/postgres: nil content")
	}
	c.UpdatedAt = time.Now().UTC()
	res, err := s.db.ExecContext(
		ctx,
		`UPDATE content SET kind = $1, slug = $2, title = $3, body = $4, body_format = $5, author_id = $6, updated_at = $7
		 WHERE id = $8 AND tenant_id = $9`,
		c.Kind, c.Slug, c.Title, c.Body, c.BodyFormat, c.AuthorID, c.UpdatedAt, c.ID, c.TenantID,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return store.ErrSlugTaken
		}
		return fmt.Errorf("content/postgres: update: %w", err)
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return store.ErrNotFound
	}
	return nil
}

// Delete removes a content row by (tenant_id, id). The tenant predicate is
// mandatory so a caller cannot delete another tenant's row by ID.
func (s *Store) Delete(ctx context.Context, tenantID, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM content WHERE id = $1 AND tenant_id = $2`, id, tenantID)
	if err != nil {
		return fmt.Errorf("content/postgres: delete: %w", err)
	}
	rows, _ := res.RowsAffected()
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
		`UPDATE content SET published_at = $1, updated_at = $2 WHERE id = $3 AND tenant_id = $4`,
		nullableTime(at), now, id, tenantID,
	)
	if err != nil {
		return fmt.Errorf("content/postgres: set published: %w", err)
	}
	rows, _ := res.RowsAffected()
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

// classifyInsertErr maps a Postgres insert error onto the store's sentinels.
// Postgres reports both a duplicate primary key and a duplicate (tenant, kind,
// slug) as SQLSTATE 23505 (unique_violation); the constraint name distinguishes
// them — content_pkey for the id, the composite unique index otherwise.
func classifyInsertErr(err error) error {
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "content_pkey") {
		return store.ErrDuplicate
	}
	if isUniqueViolation(err) {
		return store.ErrSlugTaken
	}
	return fmt.Errorf("content/postgres: insert: %w", err)
}

// isUniqueViolation reports whether err is a Postgres unique-constraint
// violation. pgx renders SQLSTATE 23505 with the text "duplicate key value
// violates unique constraint", so matching "unique" is precise here and does
// not catch NOT NULL / CHECK / FK failures.
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique") || strings.Contains(msg, "23505")
}
