// Implements: REQ-PORTS-001.
// Per: ADR-0009.
// Discipline: C-14.

package portslib

// portslib.go owns the shared port contracts (AdminRegistrar, HealthRegistrar,
// NotificationChannel) and the small value types that travel across them. The exported surface is intentionally
// minimal so downstream modules can satisfy these contracts without dragging
// in framework-specific dependencies.
//
// ADR: ADR-0009 (ports-only module communication), ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

import (
	"context"
	"net/http"
	"time"

	"github.com/septagon-oss/pk-core/pkg/observability/health"
)

// AdminPage describes a custom page registered into the admin shell.
//
// Path is an absolute URL path that must already include the admin base path
// (e.g. "/admin/tenants"). The shell matches it verbatim against the full
// request URL and renders it verbatim as a link href, so it must not be a
// base-path-relative fragment.
type AdminPage struct {
	ModuleID string
	Path     string
	Title    string
	Render   http.HandlerFunc
}

// SidebarSection describes a left-rail section in the admin shell.
type SidebarSection struct {
	ModuleID string
	Label    string
	Order    int
	Items    []SidebarItem
}

// SidebarItem is a single entry inside a SidebarSection.
//
// Path is an absolute URL path that must already include the admin base path
// (e.g. "/admin/tenants") — it is rendered verbatim as the link href so the
// link resolves to a route the shell actually serves. Do not pass a base-path-
// relative fragment: the renderer does not prepend the base path. This keeps a
// sidebar item and its target route (a registered AdminPage or entity route)
// in lockstep.
type SidebarItem struct {
	Path  string
	Label string
}

// AdminRegistrar lets modules register entity-CRUD pages and custom admin
// pages with the host application's admin shell.
type AdminRegistrar interface {
	RegisterEntityCRUD(moduleID, entityName, apiPath string) error
	RegisterPage(p AdminPage) error
	RegisterSidebarSection(s SidebarSection) error
}

// HealthRegistrar lets modules register health checks. The default in-process
// implementations bridge to pk-core's health.Registrar.
type HealthRegistrar interface {
	Register(name string, check health.Checker) error
}

// Notification is the canonical in-app notification record. Severity values
// are restricted to "info", "warning", and "critical" in v0.1.0.
type Notification struct {
	ID        string
	TenantID  string
	UserID    string
	Title     string
	Body      string
	Category  string
	Severity  string
	Data      map[string]any
	EmittedAt time.Time
}

// NotificationChannel is satisfied by the built-in in-app channel; Pro adds
// mail/SMS/push channels via the same interface.
type NotificationChannel interface {
	Name() string
	Deliver(ctx context.Context, n Notification) error
}
