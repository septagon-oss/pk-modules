package health

// entities.go owns descriptor metadata used by handlers, admin pages, and
// the catalog. The health module re-exports pk-core's Result/Status types as
// the canonical wire format so callers do not import two near-identical
// shapes.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

import "github.com/septagon-oss/pk-core/pkg/observability/health"

// EntityName is the stable display name of the aggregate health report.
const EntityName = "HealthReport"

// APIPath is the canonical HTTP base path for the health endpoint.
const APIPath = "/healthz"

// Result re-exports pk-core's aggregate report type so downstream callers
// can depend on this module's package alone.
type Result = health.Result

// ComponentResult re-exports pk-core's per-component result.
type ComponentResult = health.ComponentResult

// Status re-exports the health.Status enum.
type Status = health.Status

// Aggregate status values mirrored from pk-core for OSS callers.
const (
	StatusHealthy   = health.StatusHealthy
	StatusDegraded  = health.StatusDegraded
	StatusUnhealthy = health.StatusUnhealthy
)
