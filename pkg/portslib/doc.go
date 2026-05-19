// Package portslib defines the shared port surfaces that pk-modules business
// modules use to expose admin pages, health checks, in-app notifications,
// translations, and configurable settings.
//
// doc.go owns the package overview. The contracts in this file are intended to
// be small, value-typed, and reusable across every module so that modules can
// communicate through interfaces alone — never through direct imports of each
// other's concrete implementation.
//
// ADR: ADR-0009 (ports-only module communication), ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package portslib
