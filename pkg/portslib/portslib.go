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

// AdminFieldKind controls how an admin field is rendered and serialized.
// Values intentionally map to platform-neutral form concepts rather than a
// specific frontend library.
type AdminFieldKind string

const (
	AdminFieldText     AdminFieldKind = "text"
	AdminFieldEmail    AdminFieldKind = "email"
	AdminFieldPassword AdminFieldKind = "password"
	AdminFieldSlug     AdminFieldKind = "slug"
	AdminFieldTextarea AdminFieldKind = "textarea"
	AdminFieldSelect   AdminFieldKind = "select"
	AdminFieldBoolean  AdminFieldKind = "boolean"
	AdminFieldNumber   AdminFieldKind = "number"
	AdminFieldTags     AdminFieldKind = "tags"
	AdminFieldDateTime AdminFieldKind = "datetime"
	AdminFieldStatus   AdminFieldKind = "status"
	AdminFieldCount    AdminFieldKind = "count"
)

// AdminOption is one allowed value for a select field.
type AdminOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// AdminField describes one editable resource field.
type AdminField struct {
	Key              string         `json:"key"`
	Label            string         `json:"label"`
	Kind             AdminFieldKind `json:"kind"`
	Required         bool           `json:"required,omitempty"`
	RequiredOnCreate bool           `json:"required_on_create,omitempty"`
	ReadOnly         bool           `json:"read_only,omitempty"`
	DefaultValue     string         `json:"default_value,omitempty"`
	Placeholder      string         `json:"placeholder,omitempty"`
	Help             string         `json:"help,omitempty"`
	Min              int            `json:"min,omitempty"`
	Max              int            `json:"max,omitempty"`
	Options          []AdminOption  `json:"options,omitempty"`
}

// AdminColumn describes one human-readable table column.
type AdminColumn struct {
	Key     string         `json:"key"`
	Label   string         `json:"label"`
	Kind    AdminFieldKind `json:"kind,omitempty"`
	Primary bool           `json:"primary,omitempty"`
}

// AdminConditionOperator selects the comparison used for a row-aware admin
// control. Empty and not_empty treat nil, empty strings, and empty arrays as
// empty; equals compares the string representation with Value.
type AdminConditionOperator string

const (
	AdminConditionEquals   AdminConditionOperator = "equals"
	AdminConditionEmpty    AdminConditionOperator = "empty"
	AdminConditionNotEmpty AdminConditionOperator = "not_empty"
)

// AdminRowCondition controls whether a row-level edit, delete, or lifecycle
// action is useful for the current resource state.
type AdminRowCondition struct {
	Field    string                 `json:"field"`
	Operator AdminConditionOperator `json:"operator"`
	Value    string                 `json:"value,omitempty"`
}

// AdminAction describes a resource-row lifecycle action. PathSuffix is
// appended to "<api path>/<escaped id>" and Method defaults to POST.
type AdminAction struct {
	Label       string             `json:"label"`
	Method      string             `json:"method,omitempty"`
	PathSuffix  string             `json:"path_suffix"`
	Variant     string             `json:"variant,omitempty"`
	Confirm     string             `json:"confirm,omitempty"`
	VisibleWhen *AdminRowCondition `json:"visible_when,omitempty"`
}

// AdminResource is the complete, framework-neutral description of a managed
// API resource. The reference admin uses it to render useful columns and
// typed forms instead of exposing raw JSON editors.
type AdminResource struct {
	ModuleID      string             `json:"module_id"`
	EntityName    string             `json:"entity_name"`
	SingularLabel string             `json:"singular_label"`
	PluralLabel   string             `json:"plural_label"`
	Description   string             `json:"description,omitempty"`
	APIPath       string             `json:"api_path"`
	IDKey         string             `json:"id_key,omitempty"`
	Columns       []AdminColumn      `json:"columns"`
	Fields        []AdminField       `json:"fields,omitempty"`
	CanCreate     bool               `json:"can_create,omitempty"`
	CanEdit       bool               `json:"can_edit,omitempty"`
	EditWhen      *AdminRowCondition `json:"edit_when,omitempty"`
	CanDelete     bool               `json:"can_delete,omitempty"`
	DeleteWhen    *AdminRowCondition `json:"delete_when,omitempty"`
	Actions       []AdminAction      `json:"actions,omitempty"`
	SuccessField  string             `json:"success_field,omitempty"`
}

// AdminRegistrarContractVersion is the compatibility version of
// AdminRegistrar. The schema-aware RegisterResource API replaced the v0.3
// RegisterEntityCRUD API and therefore starts a new contract line in v0.4.
const AdminRegistrarContractVersion = "0.4.0"

// AdminRegistrar lets modules register schema-aware resources and custom
// pages with the host application's admin shell.
type AdminRegistrar interface {
	RegisterResource(resource AdminResource) error
	RegisterPage(p AdminPage) error
	RegisterSidebarSection(s SidebarSection) error
}

// HealthRegistrar lets modules register health checks. The default in-process
// implementations bridge to pk-core's health.Registrar.
type HealthRegistrar interface {
	Register(name string, check health.Checker) error
}

// Notification is the canonical in-app notification record. Severity values
// are restricted to "info", "warning", and "critical". JSON field names are
// snake_case, consistent with every other module's API surface.
type Notification struct {
	ID        string         `json:"id"`
	TenantID  string         `json:"tenant_id"`
	UserID    string         `json:"user_id"`
	Title     string         `json:"title"`
	Body      string         `json:"body"`
	Category  string         `json:"category"`
	Severity  string         `json:"severity"`
	Data      map[string]any `json:"data,omitempty"`
	EmittedAt time.Time      `json:"emitted_at"`
}

// NotificationChannel is satisfied by the built-in in-app channel; Pro adds
// mail/SMS/push channels via the same interface.
type NotificationChannel interface {
	Name() string
	Deliver(ctx context.Context, n Notification) error
}
