// Package health implements the health_management business module.
//
// doc.go owns the package overview. The module is the provider for the
// portslib.HealthRegistrar contract: other modules register checkers through
// this port and the module aggregates the results into a single /healthz HTTP
// surface. The module wraps pk-core's health.Registrar primitive so the
// underlying check semantics (timeouts, panic recovery, degraded-as-200 HTTP
// status) come for free.
//
// Health checks are runtime state, not persistent data, so this module has
// no sqlite store and no migrations.
//
// Design:
//   - Composable: catalog wires NewModule and consumes Compose() to declare
//     Provides/Requires.
//   - Chainable: pk-pro embeds *Module and adds external check forwarders,
//     SLO scoring, and tenant-scoped probes without changing the OSS contract.
//
// ADR: ADR-0009 (ports-only module communication), ADR-0017 (composition through dependency injection), ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package health
