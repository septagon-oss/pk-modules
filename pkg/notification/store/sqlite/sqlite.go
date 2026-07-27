// Package sqlite is the default Store implementation for
// notification_management backed by database/sql.
//
// sqlite.go owns the Store implementation. Callers must register their
// preferred sqlite driver (typically `_ "modernc.org/sqlite"`) before
// opening a database; this package does not import any driver so binaries
// stay slim and the driver is a build-time concern of the host application.
//
// ADR: ADR-0009 (ports-only module communication), ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package sqlite

// Implements: REQ-NOTIF-002.
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
	"github.com/septagon-oss/pk-modules/pkg/notification/store"
)

// Store is a database/sql-backed implementation of store.Store. It is safe
// for concurrent use by multiple goroutines.
// Compile-time proof the sqlite store satisfies the store.Store contract
// (Effective Go "interface checks").
var _ store.Store = (*Store)(nil)

type Store struct {
	db *sql.DB
}

// New returns a Store wrapping the given *sql.DB and ensures the
// `notifications` and `notification_subscriptions` tables exist.
func New(db *sql.DB) (*Store, error) {
	if db == nil {
		return nil, errors.New("notification/sqlite: nil *sql.DB")
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
		return nil, fmt.Errorf("notification/sqlite: open: %w", err)
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
CREATE TABLE IF NOT EXISTS notifications (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    user_id TEXT NOT NULL,
    title TEXT NOT NULL,
    body TEXT NOT NULL,
    category TEXT,
    severity TEXT NOT NULL,
    data TEXT,
    read_at DATETIME,
    emitted_at DATETIME NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_notifications_user ON notifications(user_id, emitted_at);

CREATE TABLE IF NOT EXISTS notification_subscriptions (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL,
    user_id TEXT NOT NULL,
    category TEXT,
    channel TEXT NOT NULL,
    created_at DATETIME NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_notif_subs_user ON notification_subscriptions(user_id);
`

// migrations is this adapter's schema history. 0001 is the schema exactly as
// it shipped before the ledger existed, so a database created by an earlier
// release adopts it as a recorded no-op rather than a rebuild. Never edit an
// applied migration — add the next one.
var migrations = []migrate.Migration{
	{Name: "0001_create_notification", SQL: schemaDDL},
}

func (s *Store) ensureSchema(ctx context.Context) error {
	return migrate.Run(ctx, s.db, migrate.Options{Module: "notification", Postgres: false}, migrations)
}

const selectNotificationColumns = `SELECT id, tenant_id, user_id, title, body, category, severity, data, read_at, emitted_at FROM notifications`

// Create inserts a notification row.
func (s *Store) Create(ctx context.Context, n *store.Notification) error {
	if n == nil {
		return errors.New("notification/sqlite: nil notification")
	}
	if n.EmittedAt.IsZero() {
		n.EmittedAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(
		ctx,
		`INSERT INTO notifications (id, tenant_id, user_id, title, body, category, severity, data, read_at, emitted_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		n.ID, n.TenantID, n.UserID, n.Title, n.Body, nullString(n.Category), n.Severity,
		nullString(n.Data), nullableTime(n.ReadAt), n.EmittedAt,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return store.ErrDuplicate
		}
		return fmt.Errorf("notification/sqlite: insert: %w", err)
	}
	return nil
}

// GetByUser returns notifications for a user ordered by emitted_at DESC.
func (s *Store) GetByUser(ctx context.Context, tenantID, userID string, limit, offset int) ([]*store.Notification, error) {
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
	rows, err := s.db.QueryContext(
		ctx,
		selectNotificationColumns+` WHERE tenant_id = ? AND user_id = ? ORDER BY emitted_at DESC LIMIT ? OFFSET ?`,
		tenantID, userID, limit, offset,
	)
	if err != nil {
		return nil, fmt.Errorf("notification/sqlite: get by user: %w", err)
	}
	defer rows.Close()
	var out []*store.Notification
	for rows.Next() {
		n, err := scanNotificationRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("notification/sqlite: get by user rows: %w", err)
	}
	return out, nil
}

// MarkRead sets the read_at timestamp for the notification owned by
// (tenantID, userID) with the given id. The tenant AND user predicates are
// mandatory so a caller cannot mark another user's (even a tenant-mate's)
// notification read by ID.
func (s *Store) MarkRead(ctx context.Context, tenantID, userID, id string, at time.Time) error {
	if at.IsZero() {
		at = time.Now().UTC()
	}
	res, err := s.db.ExecContext(
		ctx,
		`UPDATE notifications SET read_at = ? WHERE id = ? AND tenant_id = ? AND user_id = ?`,
		at, id, tenantID, userID,
	)
	if err != nil {
		return fmt.Errorf("notification/sqlite: mark read: %w", err)
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return store.ErrNotFound
	}
	return nil
}

const selectSubscriptionColumns = `SELECT id, tenant_id, user_id, category, channel, created_at FROM notification_subscriptions`

// AddSubscription inserts a subscription row.
func (s *Store) AddSubscription(ctx context.Context, sub *store.Subscription) error {
	if sub == nil {
		return errors.New("notification/sqlite: nil subscription")
	}
	if sub.CreatedAt.IsZero() {
		sub.CreatedAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(
		ctx,
		`INSERT INTO notification_subscriptions (id, tenant_id, user_id, category, channel, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		sub.ID, sub.TenantID, sub.UserID, nullString(sub.Category), sub.Channel, sub.CreatedAt,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return store.ErrDuplicate
		}
		return fmt.Errorf("notification/sqlite: add subscription: %w", err)
	}
	return nil
}

// RemoveSubscription deletes the subscription identified by (tenantID, id). The
// tenant predicate is mandatory so a caller cannot remove another tenant's
// subscription by ID.
func (s *Store) RemoveSubscription(ctx context.Context, tenantID, userID, id string) error {
	res, err := s.db.ExecContext(
		ctx,
		`DELETE FROM notification_subscriptions WHERE id = ? AND tenant_id = ? AND user_id = ?`, id, tenantID, userID,
	)
	if err != nil {
		return fmt.Errorf("notification/sqlite: remove subscription: %w", err)
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return store.ErrNotFound
	}
	return nil
}

// ListSubscriptions returns every subscription for the given (tenantID, userID).
// The tenant predicate is mandatory so a caller cannot enumerate another
// tenant's subscriptions.
func (s *Store) ListSubscriptions(ctx context.Context, tenantID, userID string) ([]*store.Subscription, error) {
	rows, err := s.db.QueryContext(
		ctx,
		selectSubscriptionColumns+` WHERE tenant_id = ? AND user_id = ? ORDER BY created_at ASC`,
		tenantID, userID,
	)
	if err != nil {
		return nil, fmt.Errorf("notification/sqlite: list subscriptions: %w", err)
	}
	defer rows.Close()
	var out []*store.Subscription
	for rows.Next() {
		sub, err := scanSubscriptionRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sub)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("notification/sqlite: list subscription rows: %w", err)
	}
	return out, nil
}

func scanNotificationRows(row *sql.Rows) (*store.Notification, error) {
	n := &store.Notification{}
	var (
		category sql.NullString
		data     sql.NullString
		readAt   sql.NullTime
	)
	if err := row.Scan(
		&n.ID, &n.TenantID, &n.UserID, &n.Title, &n.Body, &category, &n.Severity,
		&data, &readAt, &n.EmittedAt,
	); err != nil {
		return nil, fmt.Errorf("notification/sqlite: scan notification: %w", err)
	}
	if category.Valid {
		n.Category = category.String
	}
	if data.Valid {
		n.Data = data.String
	}
	if readAt.Valid {
		t := readAt.Time
		n.ReadAt = &t
	}
	return n, nil
}

func scanSubscriptionRows(row *sql.Rows) (*store.Subscription, error) {
	sub := &store.Subscription{}
	var category sql.NullString
	if err := row.Scan(
		&sub.ID, &sub.TenantID, &sub.UserID, &category, &sub.Channel, &sub.CreatedAt,
	); err != nil {
		return nil, fmt.Errorf("notification/sqlite: scan subscription: %w", err)
	}
	if category.Valid {
		sub.Category = category.String
	}
	return sub, nil
}

func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
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
	// Match only UNIQUE violations. sqlite phrases them "UNIQUE constraint
	// failed: ..." and Postgres "violates unique constraint"; both contain
	// "unique". Matching bare "constraint" was too broad — it mislabeled NOT
	// NULL/CHECK/FK failures as duplicates (a spurious 409).
	return strings.Contains(msg, "unique")
}
