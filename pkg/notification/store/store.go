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
type Store interface {
	Create(ctx context.Context, n *Notification) error
	GetByUser(ctx context.Context, userID string, limit, offset int) ([]*Notification, error)
	MarkRead(ctx context.Context, id string, at time.Time) error

	AddSubscription(ctx context.Context, s *Subscription) error
	RemoveSubscription(ctx context.Context, id string) error
	ListSubscriptions(ctx context.Context, userID string) ([]*Subscription, error)
}

// Sentinel errors returned by Store implementations.
var (
	ErrNotFound  = errors.New("notification: not found")
	ErrDuplicate = errors.New("notification: duplicate id")
)
