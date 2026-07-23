package audit

// Implements: REQ-AUDIT-001.
// Per: ADR-0017.
// Discipline: C-14.
// ports.go owns the public interfaces other modules consume. Downstream
// modules should depend on these interfaces — never on *Module or the
// concrete store implementations.
//
// ADR: ADR-0009 (ports-only module communication), ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

import (
	"context"
	"time"
)

// AuditService is the write-side port. Other modules call Record to append
// events to the tenant-scoped audit log.
type AuditService interface {
	Record(ctx context.Context, e *Event) error
}

// AuditReader is the read-side port for admin pages, dashboards, and Pro
// retention enforcers.
type AuditReader interface {
	Query(ctx context.Context, filter QueryFilter) ([]*Event, error)
}

// QueryFilter is the structured filter accepted by AuditReader.Query. Fields
// left at their zero value are treated as "any".
type QueryFilter struct {
	TenantID string
	Actor    string
	Action   string
	Since    time.Time
	Until    time.Time
	Limit    int
	Offset   int
}

// AuditEmitter is a convenience for non-audit modules that want to emit
// events without holding a full AuditService reference. The implementation
// wraps Record and JSON-encodes the details payload.
type AuditEmitter interface {
	Emit(ctx context.Context, action, resource string, details map[string]any) error
}
