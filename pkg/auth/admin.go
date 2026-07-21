package auth

// Implements: REQ-AUTH-001.
// Per: ADR-0028.
// Discipline: C-14.
// admin.go owns the admin shell wiring for auth_management: the sidebar
// section, the entity-CRUD registration for sessions, and a small
// server-rendered page stub that describes the auth API surface.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

import (
	"fmt"
	"html"
	"html/template"
	"net/http"

	"github.com/septagon-oss/pk-modules/pkg/portslib"
)

// adminModuleID is the catalog ID used when registering admin surfaces.
const adminModuleID = "auth_management"

// AdminPagePath is the canonical path under which the auth admin page is
// mounted by registerAdmin.
const AdminPagePath = "/admin/auth"

var dashboardTemplate = template.Must(template.New("auth-admin").Parse(`
<!doctype html>
<html lang="en">
<head><meta charset="utf-8"><title>{{.Title}}</title></head>
<body>
<h1>{{.Title}}</h1>
<p>{{.Description}}</p>
<p>Sessions API: <code>{{.APIPath}}</code></p>
</body>
</html>
`))

type dashboardData struct {
	Title       string
	Description string
	APIPath     string
}

func renderAdminDashboard() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		err := dashboardTemplate.Execute(w, dashboardData{
			Title:       "Authentication",
			Description: "Session-cookie login and session lifecycle for PlatformKit.",
			APIPath:     html.EscapeString(APIPath),
		})
		if err != nil {
			http.Error(w, fmt.Sprintf("render: %v", err), http.StatusInternalServerError)
		}
	}
}

func registerAdmin(r portslib.AdminRegistrar) error {
	if r == nil {
		return nil
	}
	if err := r.RegisterEntityCRUD(adminModuleID, EntityName, APIPath); err != nil {
		return fmt.Errorf("auth: admin entity CRUD: %w", err)
	}
	if err := r.RegisterPage(portslib.AdminPage{
		ModuleID: adminModuleID,
		Path:     AdminPagePath,
		Title:    "Authentication",
		Render:   renderAdminDashboard(),
	}); err != nil {
		return fmt.Errorf("auth: admin page: %w", err)
	}
	if err := r.RegisterSidebarSection(portslib.SidebarSection{
		ModuleID: adminModuleID,
		Label:    "Auth",
		Order:    25,
		Items: []portslib.SidebarItem{
			{Path: AdminPagePath, Label: "Sessions"},
		},
	}); err != nil {
		return fmt.Errorf("auth: admin sidebar: %w", err)
	}
	return nil
}
