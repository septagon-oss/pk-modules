// Package sqlite is the default Store implementation for audit_management
// backed by database/sql.
//
// sqlite.go owns the append-only audit log implementation. Callers must
// register their preferred sqlite driver (typically `_ "modernc.org/sqlite"`)
// before opening a database; this package does not import any driver so
// binaries stay slim and the driver is a build-time concern of the host
// application.
//
// ADR: ADR-0009 (ports-only module communication), ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package sqlite

// Implements: REQ-AUDIT-001.
// Per: ADR-0017.
// Discipline: C-14.
import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/septagon-oss/pk-modules/pkg/audit/store"
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
// `audit_events` table exists, so callers can pair a fresh in-memory database
// with the store without running the migration files explicitly.
func New(db *sql.DB) (*Store, error) {
	if db == nil {
		return nil, errors.New("audit/sqlite: nil *sql.DB")
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
		return nil, fmt.Errorf("audit/sqlite: open: %w", err)
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
    emitted_at DATETIME NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_audit_events_tenant_emitted ON audit_events(tenant_id, emitted_at);
CREATE INDEX IF NOT EXISTS idx_audit_events_actor_emitted ON audit_events(actor, emitted_at);
`

func (s *Store) ensureSchema(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, schemaDDL); err != nil {
		return fmt.Errorf("audit/sqlite: ensure schema: %w", err)
	}
	return nil
}

// Append inserts an audit event. Returns store.ErrDuplicateID when the ID is
// already in use.
func (s *Store) Append(ctx context.Context, e *store.Event) error {
	if e == nil {
		return errors.New("audit/sqlite: nil event")
	}
	if e.EmittedAt.IsZero() {
		e.EmittedAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(
		ctx,
		`INSERT INTO audit_events (id, tenant_id, actor, action, resource, severity, details, emitted_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		e.ID, e.TenantID, e.Actor, e.Action, e.Resource, e.Severity, e.Details, e.EmittedAt,
	)
	if err != nil {
		if isUniqueConstraint(err) {
			return store.ErrDuplicateID
		}
		return fmt.Errorf("audit/sqlite: append: %w", err)
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
		clauses = []string{"tenant_id = ?"}
		args    = []any{f.TenantID}
	)
	if f.Actor != "" {
		clauses = append(clauses, "actor = ?")
		args = append(args, f.Actor)
	}
	if f.Action != "" {
		clauses = append(clauses, "action = ?")
		args = append(args, f.Action)
	}
	if !f.Since.IsZero() {
		clauses = append(clauses, "emitted_at >= ?")
		args = append(args, f.Since)
	}
	if !f.Until.IsZero() {
		clauses = append(clauses, "emitted_at < ?")
		args = append(args, f.Until)
	}

	where := ""
	if len(clauses) > 0 {
		where = " WHERE " + strings.Join(clauses, " AND ")
	}
	args = append(args, limit, offset)

	rows, err := s.db.QueryContext(ctx,
		`SELECT id, tenant_id, actor, action, resource, severity, details, emitted_at
		 FROM audit_events`+where+` ORDER BY emitted_at ASC LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return nil, fmt.Errorf("audit/sqlite: query: %w", err)
	}
	defer rows.Close()

	var out []*store.Event
	for rows.Next() {
		e := &store.Event{}
		if err := rows.Scan(&e.ID, &e.TenantID, &e.Actor, &e.Action, &e.Resource, &e.Severity, &e.Details, &e.EmittedAt); err != nil {
			return nil, fmt.Errorf("audit/sqlite: scan: %w", err)
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("audit/sqlite: query rows: %w", err)
	}
	return out, nil
}

func isUniqueConstraint(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique constraint") ||
		strings.Contains(msg, "constraint failed: unique") ||
		strings.Contains(msg, "primary key")
}
