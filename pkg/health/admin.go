package health

// Implements: REQ-HEALTH-001.
// Per: ADR-0017.
// Discipline: C-14.
// admin.go owns the admin shell wiring for health_management: the sidebar
// section and a small server-rendered page stub that surfaces the aggregate
// health report.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"

	"github.com/septagon-oss/pk-modules/pkg/portslib"
)

// adminModuleID is the catalog ID used when registering admin surfaces.
const adminModuleID = "health_management"

// AdminPagePath is the canonical path under which the health admin page is
// mounted by registerAdmin.
const AdminPagePath = "/admin/health"

var dashboardTemplate = template.Must(template.New("health-admin").Parse(`
<!doctype html>
<html lang="en">
<head><meta charset="utf-8"><title>{{.Title}}</title></head>
<body>
<h1>{{.Title}}</h1>
<p>Status: <strong>{{.Status}}</strong></p>
<pre>{{.Body}}</pre>
</body>
</html>
`))

type dashboardData struct {
	Title  string
	Status string
	Body   string
}

func renderAdminDashboard(svc HealthService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		res := svc.Check(r.Context())
		body, err := json.MarshalIndent(res, "", "  ")
		if err != nil {
			http.Error(w, fmt.Sprintf("marshal: %v", err), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := dashboardTemplate.Execute(w, dashboardData{
			Title:  "Health",
			Status: string(res.Status),
			Body:   string(body),
		}); err != nil {
			http.Error(w, fmt.Sprintf("render: %v", err), http.StatusInternalServerError)
		}
	}
}

func registerAdmin(r portslib.AdminRegistrar, svc HealthService) error {
	if r == nil {
		return nil
	}
	if err := r.RegisterPage(portslib.AdminPage{
		ModuleID: adminModuleID,
		Path:     AdminPagePath,
		Title:    "Health",
		Render:   renderAdminDashboard(svc),
	}); err != nil {
		return fmt.Errorf("health: admin page: %w", err)
	}
	if err := r.RegisterSidebarSection(portslib.SidebarSection{
		ModuleID: adminModuleID,
		Label:    "Health",
		Order:    80,
		Items: []portslib.SidebarItem{
			{Path: AdminPagePath, Label: "Status"},
		},
	}); err != nil {
		return fmt.Errorf("health: admin sidebar: %w", err)
	}
	return nil
}
