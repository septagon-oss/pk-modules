package tenant

// admin.go owns the admin shell wiring for tenant_management: the sidebar
// section, the entity-CRUD registration, and a small server-rendered page
// stub that hosts the dashboard listing.
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
const adminModuleID = "tenant_management"

// AdminPagePath is the canonical path under which the tenants admin page is
// mounted by registerAdmin.
const AdminPagePath = "/admin/tenants"

// dashboardTemplate is a tiny stub the OSS module renders for the admin
// dashboard. Pro replaces it with a richer SSR/SPA composition; the OSS
// surface stays self-contained.
var dashboardTemplate = template.Must(template.New("tenants-admin").Parse(`
<!doctype html>
<html lang="en">
<head><meta charset="utf-8"><title>{{.Title}}</title></head>
<body>
<h1>{{.Title}}</h1>
<p>{{.Description}}</p>
<p>Tenants API: <code>{{.APIPath}}</code></p>
</body>
</html>
`))

type dashboardData struct {
	Title       string
	Description string
	APIPath     string
}

// renderAdminDashboard returns the http.HandlerFunc that backs the admin
// landing page. Exposed for tests.
func renderAdminDashboard() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		err := dashboardTemplate.Execute(w, dashboardData{
			Title:       "Tenants",
			Description: "Manage tenants for this PlatformKit application.",
			APIPath:     html.EscapeString(APIPath),
		})
		if err != nil {
			http.Error(w, fmt.Sprintf("render: %v", err), http.StatusInternalServerError)
		}
	}
}

// registerAdmin registers the entity-CRUD page, the dashboard page, and the
// sidebar section with the provided AdminRegistrar.
func registerAdmin(r portslib.AdminRegistrar) error {
	if r == nil {
		return nil
	}
	if err := r.RegisterEntityCRUD(adminModuleID, EntityName, APIPath); err != nil {
		return fmt.Errorf("tenant: admin entity CRUD: %w", err)
	}
	if err := r.RegisterPage(portslib.AdminPage{
		ModuleID: adminModuleID,
		Path:     AdminPagePath,
		Title:    "Tenants",
		Render:   renderAdminDashboard(),
	}); err != nil {
		return fmt.Errorf("tenant: admin page: %w", err)
	}
	if err := r.RegisterSidebarSection(portslib.SidebarSection{
		ModuleID: adminModuleID,
		Label:    "Tenants",
		Order:    10,
		Items: []portslib.SidebarItem{
			{Path: AdminPagePath, Label: "All tenants"},
		},
	}); err != nil {
		return fmt.Errorf("tenant: admin sidebar: %w", err)
	}
	return nil
}
