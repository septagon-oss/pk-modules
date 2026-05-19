// Package store defines the persistence contract for audit_management.
//
// store.go owns the Store interface and shared sentinel errors. Concrete
// implementations live in subpackages such as store/sqlite.
//
// ADR: ADR-0009 (ports-only module communication), ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package store

import (
	"context"
	"errors"
	"time"
)

// Event is the persisted shape of an audit event. It mirrors the public
// audit.Event entity but is duplicated here so the store layer does not
// import the parent package (which would create a cycle).
type Event struct {
	ID        string
	TenantID  string
	Actor     string
	Action    string
	Resource  string
	Severity  string
	Details   string
	EmittedAt time.Time
}

// QueryFilter is the structured filter accepted by Store.Query. Fields left
// at their zero value are treated as "any".
type QueryFilter struct {
	TenantID string
	Actor    string
	Action   string
	Since    time.Time
	Until    time.Time
	Limit    int
}

// Store is the persistence contract for audit_management. The OSS surface is
// append-only; update/delete are deliberately omitted.
type Store interface {
	Append(ctx context.Context, e *Event) error
	Query(ctx context.Context, f QueryFilter) ([]*Event, error)
}

// Sentinel errors returned by Store implementations.
var (
	ErrDuplicateID = errors.New("audit: duplicate id")
)
