package health

// Implements: REQ-HEALTH-001.
// Per: ADR-0017.
// Discipline: C-14.
// ports.go owns the public interfaces other modules consume from this module.
// The module is the canonical provider of portslib.HealthRegistrar; callers
// that need a richer surface (eg. one-shot Check()) can use HealthService.
//
// ADR: ADR-0009 (ports-only module communication), ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

import "context"

// HealthService is the Pro-consumable port that exposes aggregate evaluation
// of every registered checker. Most callers register checks through the
// portslib.HealthRegistrar bridge and never need to call HealthService
// directly; admin pages and Pro retention layers do.
type HealthService interface {
	Check(ctx context.Context) Result
}
