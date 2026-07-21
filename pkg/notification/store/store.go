// Package store defines the persistence contract for notification_management.
//
// store.go owns the Store interface, the persisted row shapes, and shared
// sentinel errors. Concrete implementations live in subpackages such as
// store/sqlite.
//
// ADR: ADR-0009 (ports-only module communication), ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package store

import (
	"context"
	"errors"
	"time"
)

// Notification is the persisted shape of a notification row. It mirrors
// portslib.Notification but is duplicated here so the store layer does not
// import portslib (which would create a cycle when adapters import the store
// directly).
type Notification struct {
	ID        string
	TenantID  string
	UserID    string
	Title     string
	Body      string
	Category  string
	Severity  string
	Data      string // JSON-encoded
	ReadAt    *time.Time
	EmittedAt time.Time
}

// Subscription is the persisted shape of a subscription row.
type Subscription struct {
	ID        string
	TenantID  string
	UserID    string
	Category  string
	Channel   string
	CreatedAt time.Time
}

// Store is the persistence contract for notification_management.
// Implementations must be safe for concurrent use by multiple goroutines.
type Store interface {
	// Create persists a new notification, defaulting EmittedAt when unset.
	Create(ctx context.Context, n *Notification) error
	// GetByUser returns a (tenant, user)'s notifications ordered by emission
	// time descending, paged by limit and offset. The tenant predicate is
	// mandatory so a caller cannot read another tenant's notifications.
	GetByUser(ctx context.Context, tenantID, userID string, limit, offset int) ([]*Notification, error)
	// MarkRead records the read timestamp for the notification owned by
	// (tenantID, userID) with the given id. The tenant AND user predicates are
	// mandatory: a caller cannot mark another user's notification read, even a
	// tenant-mate's. Returns ErrNotFound when no matching notification exists.
	MarkRead(ctx context.Context, tenantID, userID, id string, at time.Time) error

	// AddSubscription persists a new subscription. Implementations return
	// ErrDuplicate when the subscription ID is already in use.
	AddSubscription(ctx context.Context, s *Subscription) error
	// RemoveSubscription deletes the subscription owned by (tenantID, userID)
	// with the given id. The tenant AND user predicates are mandatory: a caller
	// cannot delete another user's subscription. Returns ErrNotFound when none
	// matches.
	RemoveSubscription(ctx context.Context, tenantID, userID, id string) error
	// ListSubscriptions returns every subscription for the given (tenant, user).
	ListSubscriptions(ctx context.Context, tenantID, userID string) ([]*Subscription, error)
}

// Sentinel errors returned by Store implementations.
var (
	ErrNotFound  = errors.New("notification: not found")
	ErrDuplicate = errors.New("notification: duplicate id")
)
