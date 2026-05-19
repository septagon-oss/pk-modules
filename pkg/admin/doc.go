// Package admin implements the admin_management plugin module.
//
// doc.go owns the package overview. The module is the canonical provider of
// the portslib.AdminRegistrar contract: every other business module declares
// an optional dependency on AdminRegistrar with preferred provider
// "admin_management", and registers entity-CRUD pages, custom pages, and
// sidebar sections through that port. The admin module collects those
// registrations into an in-memory shell and renders a small server-rendered
// HTML admin under /admin.
//
// Admin state is purely runtime — there is no sqlite store and no migrations.
// All wiring happens at boot when sibling modules call into the registrar.
//
// Design:
//
//   - Identity: stable package path github.com/septagon-oss/pk-modules/pkg/admin
//     and module ID "admin_management".
//
//   - Boundary: the Shell type holds registered pages, sidebar sections, and
//     entity CRUDs. Its mutable state is private; the public surface is the
//     portslib.AdminRegistrar interface plus an http.Handler. Templates and
//     static assets are embedded into the binary so the plugin is self-
//     contained.
//
//   - Contract: portslib.AdminRegistrar. Other modules talk to this module
//     only through that interface; they never import this package directly.
//
//   - Composition: catalog wires NewModule and consumes Compose() to declare
//     Provides[portslib.AdminRegistrar]. App composition feeds Registrar()
//     into the other eight essentials modules via their respective
//     WithAdminRegistrar options.
//
//   - Replacement: ANY value satisfying portslib.AdminRegistrar can replace
//     this admin shell. A Pro Vue/React admin replaces this module by
//     constructing its own registrar and skipping admin_management in the
//     module catalog — the OSS surface is intentionally swappable.
//
//   - Extension: pk-pro embeds *Module to add Pro-only fields/methods and
//     can override the shell title, attach a richer rendering pipeline, or
//     wholesale replace ServeHTTP via composition.
//
//   - Runtime binding: app composition mounts the HTTPHandler at the
//     configured base path (default /admin).
//
//   - Evidence: module_test.go and extension_example_test.go in the
//     external admin_test package validate the public contract.
//
// ADR: ADR-0009 (ports-only module communication), ADR-0017 (composition through dependency injection), ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package admin
