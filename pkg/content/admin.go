package content

// Implements: REQ-CONTENT-001.
// Per: ADR-0017.
// Discipline: C-14.
// admin.go owns the admin shell wiring for content_management: the sidebar
// section, the entity-CRUD registration, and a small server-rendered page
// stub that hosts the content authoring dashboard.
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
const adminModuleID = "content_management"

// AdminPagePath is the canonical path under which the content admin page is
// mounted by registerAdmin.
const AdminPagePath = "/admin/content"

var dashboardTemplate = template.Must(template.New("content-admin").Parse(`
<!doctype html>
<html lang="en">
<head><meta charset="utf-8"><title>{{.Title}}</title></head>
<body>
<h1>{{.Title}}</h1>
<p>{{.Description}}</p>
<p>Content API: <code>{{.APIPath}}</code></p>
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
			Title:       "Content",
			Description: "Tenant-scoped pages, posts, and snippets.",
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
		return fmt.Errorf("content: admin entity CRUD: %w", err)
	}
	if err := r.RegisterPage(portslib.AdminPage{
		ModuleID: adminModuleID,
		Path:     AdminPagePath,
		Title:    "Content",
		Render:   renderAdminDashboard(),
	}); err != nil {
		return fmt.Errorf("content: admin page: %w", err)
	}
	if err := r.RegisterSidebarSection(portslib.SidebarSection{
		ModuleID: adminModuleID,
		Label:    "Content",
		Order:    40,
		Items: []portslib.SidebarItem{
			{Path: AdminPagePath, Label: "All content"},
		},
	}); err != nil {
		return fmt.Errorf("content: admin sidebar: %w", err)
	}
	return nil
}
