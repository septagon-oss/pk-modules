package apikey

// Implements: REQ-APIKEY-001.
// Per: ADR-0017.
// Discipline: C-14.
// admin.go owns the admin shell wiring for api_key_management: the
// sidebar section, the entity-CRUD registration, and a small server-
// rendered page stub that hosts the listing.
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
const adminModuleID = "api_key_management"

// AdminPagePath is the canonical path under which the API keys admin page
// is mounted by registerAdmin.
const AdminPagePath = "/admin/api-keys"

var dashboardTemplate = template.Must(template.New("apikey-admin").Parse(`
<!doctype html>
<html lang="en">
<head><meta charset="utf-8"><title>{{.Title}}</title></head>
<body>
<h1>{{.Title}}</h1>
<p>{{.Description}}</p>
<p>API keys: <code>{{.APIPath}}</code></p>
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
			Title:       "API keys",
			Description: "Issue, list, and revoke tenant-scoped API keys.",
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
		return fmt.Errorf("apikey: admin entity CRUD: %w", err)
	}
	if err := r.RegisterPage(portslib.AdminPage{
		ModuleID: adminModuleID,
		Path:     AdminPagePath,
		Title:    "API keys",
		Render:   renderAdminDashboard(),
	}); err != nil {
		return fmt.Errorf("apikey: admin page: %w", err)
	}
	if err := r.RegisterSidebarSection(portslib.SidebarSection{
		ModuleID: adminModuleID,
		Label:    "API keys",
		Order:    30,
		Items: []portslib.SidebarItem{
			{Path: AdminPagePath, Label: "All keys"},
		},
	}); err != nil {
		return fmt.Errorf("apikey: admin sidebar: %w", err)
	}
	return nil
}
