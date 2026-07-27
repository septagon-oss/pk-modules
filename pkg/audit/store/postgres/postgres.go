// Package postgres is the Postgres Store implementation for audit_management
// backed by database/sql.
//
// postgres.go is the production sibling of store/sqlite: the same append-only
// Store contract, the same mandatory tenant predicate on every read, the same
// fail-closed behaviour for an unscoped query — only the SQL dialect differs
// ($N placeholders, TIMESTAMPTZ columns, SQLSTATE-based error classification).
// Callers register a Postgres driver (typically
// `_ "github.com/jackc/pgx/v5/stdlib"`, driver name "pgx") before opening a
// database; this package imports no driver so the binary stays slim and the
// driver is the host application's build-time choice.
//
// ADR: ADR-0009 (ports-only module communication), ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package postgres

// Implements: REQ-AUDIT-001.
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

	"github.com/septagon-oss/pk-modules/pkg/audit/store"
	"github.com/septagon-oss/pk-modules/pkg/migrate"
)

// Store is a database/sql-backed implementation of store.Store for Postgres.
// It is safe for concurrent use by multiple goroutines.
type Store struct {
	db *sql.DB
}

// New returns a Store wrapping the given *sql.DB and ensures the
// `audit_events` table exists, so callers can pair a fresh database with the
// store without running the migration files explicitly.
func New(db *sql.DB) (*Store, error) {
	if db == nil {
		return nil, errors.New("audit/postgres: nil *sql.DB")
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
		return nil, fmt.Errorf("audit/postgres: open: %w", err)
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
CREATE TABLE IF NOT EXISTS audit_events (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    actor TEXT NOT NULL,
    action TEXT NOT NULL,
    resource TEXT NOT NULL DEFAULT '',
    severity TEXT NOT NULL DEFAULT 'info',
    details TEXT NOT NULL DEFAULT '',
    emitted_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_audit_events_tenant_emitted ON audit_events(tenant_id, emitted_at);
CREATE INDEX IF NOT EXISTS idx_audit_events_actor_emitted ON audit_events(actor, emitted_at);
`

// migrations is this adapter's schema history. 0001 is the schema exactly as
// it shipped before the ledger existed, so a database created by an earlier
// release adopts it as a recorded no-op rather than a rebuild. Never edit an
// applied migration — add the next one.
var migrations = []migrate.Migration{
	{Name: "0001_create_audit", SQL: schemaDDL},
}

func (s *Store) ensureSchema(ctx context.Context) error {
	return migrate.Run(ctx, s.db, migrate.Options{Module: "audit", Postgres: true}, migrations)
}

// Append inserts an audit event. Returns store.ErrDuplicateID when the ID is
// already in use.
func (s *Store) Append(ctx context.Context, e *store.Event) error {
	if e == nil {
		return errors.New("audit/postgres: nil event")
	}
	if e.EmittedAt.IsZero() {
		e.EmittedAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(
		ctx,
		`INSERT INTO audit_events (id, tenant_id, actor, action, resource, severity, details, emitted_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		e.ID, e.TenantID, e.Actor, e.Action, e.Resource, e.Severity, e.Details, e.EmittedAt,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return store.ErrDuplicateID
		}
		return fmt.Errorf("audit/postgres: append: %w", err)
	}
	return nil
}

// Query returns matching events ordered by emitted_at ASC (oldest first) so
// callers see chronological history. Limit is capped at 1000 by default to
// avoid runaway scans; passing 0 uses the default. Negative offsets are
// normalized to zero.
func (s *Store) Query(ctx context.Context, f store.QueryFilter) ([]*store.Event, error) {
	const defaultLimit = 100
	const maxLimit = 1000

	limit := f.Limit
	if limit <= 0 {
		limit = defaultLimit
	}
	if limit > maxLimit {
		limit = maxLimit
	}
	offset := f.Offset
	if offset < 0 {
		offset = 0
	}

	// Tenant scoping is mandatory on reads: an unscoped query must never return
	// another tenant's audit events. An empty tenant fails closed (no rows)
	// rather than returning the whole log.
	if strings.TrimSpace(f.TenantID) == "" {
		return nil, nil
	}

	var (
		clauses = []string{"tenant_id = $1"}
		args    = []any{f.TenantID}
	)
	next := 2
	if f.Actor != "" {
		clauses = append(clauses, "actor = $"+strconv.Itoa(next))
		args = append(args, f.Actor)
		next++
	}
	if f.Action != "" {
		clauses = append(clauses, "action = $"+strconv.Itoa(next))
		args = append(args, f.Action)
		next++
	}
	if !f.Since.IsZero() {
		clauses = append(clauses, "emitted_at >= $"+strconv.Itoa(next))
		args = append(args, f.Since)
		next++
	}
	if !f.Until.IsZero() {
		clauses = append(clauses, "emitted_at < $"+strconv.Itoa(next))
		args = append(args, f.Until)
		next++
	}

	where := " WHERE " + strings.Join(clauses, " AND ")
	limitPlaceholder := "$" + strconv.Itoa(next)
	offsetPlaceholder := "$" + strconv.Itoa(next+1)
	args = append(args, limit, offset)

	rows, err := s.db.QueryContext(ctx,
		`SELECT id, tenant_id, actor, action, resource, severity, details, emitted_at
		 FROM audit_events`+where+` ORDER BY emitted_at ASC LIMIT `+limitPlaceholder+` OFFSET `+offsetPlaceholder, args...)
	if err != nil {
		return nil, fmt.Errorf("audit/postgres: query: %w", err)
	}
	defer rows.Close()

	var out []*store.Event
	for rows.Next() {
		e := &store.Event{}
		if err := rows.Scan(&e.ID, &e.TenantID, &e.Actor, &e.Action, &e.Resource, &e.Severity, &e.Details, &e.EmittedAt); err != nil {
			return nil, fmt.Errorf("audit/postgres: scan: %w", err)
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("audit/postgres: query rows: %w", err)
	}
	return out, nil
}

// isUniqueViolation reports whether err is a Postgres unique-constraint
// violation. audit_events carries a single unique constraint — the primary key
// on id — so, mirroring the sqlite adapter's minimal classification, any unique
// violation on Append is a duplicate ID. pgx renders SQLSTATE 23505 with the
// text "duplicate key value violates unique constraint", so matching "unique"
// is precise here and does not catch NOT NULL / CHECK / FK failures.
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique") || strings.Contains(msg, "23505")
}
