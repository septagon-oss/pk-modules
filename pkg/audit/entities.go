package audit

// Implements: REQ-AUDIT-001.
// Per: ADR-0017.
// Discipline: C-14.
// entities.go owns the Event entity, severity constants, and descriptor
// metadata used by handlers, the store layer, and admin pages.
//
// ADR: ADR-0009 (ports-only module communication), ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

import (
	"errors"
	"strings"
	"time"
)

// Event is the entity managed by the audit_management module. Records are
// append-only; updates and deletes are not part of the OSS surface.
type Event struct {
	ID        string    `json:"id"`
	TenantID  string    `json:"tenant_id"`
	Actor     string    `json:"actor"`
	Action    string    `json:"action"`
	Resource  string    `json:"resource"`
	Severity  string    `json:"severity"`
	Details   string    `json:"details"`
	EmittedAt time.Time `json:"emitted_at"`
}

// Severity values accepted by the OSS audit module.
const (
	SeverityInfo     = "info"
	SeverityWarning  = "warning"
	SeverityCritical = "critical"
)

// EntityName is the stable display name of the Event entity.
const EntityName = "AuditEvent"

// APIPath is the canonical HTTP base path for audit query.
const APIPath = "/api/v1/audit-events"

// Validate enforces the small invariants required for storage. ID is
// caller-assigned; defaults are applied by the service layer before write.
func (e *Event) Validate() error {
	if e == nil {
		return errors.New("audit: nil")
	}
	if strings.TrimSpace(e.TenantID) == "" {
		return errors.New("audit: tenant_id is required")
	}
	if strings.TrimSpace(e.Action) == "" {
		return errors.New("audit: action is required")
	}
	switch e.Severity {
	case "", SeverityInfo, SeverityWarning, SeverityCritical:
	default:
		return errors.New("audit: severity must be info, warning, or critical")
	}
	return nil
}
