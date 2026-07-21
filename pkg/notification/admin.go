package notification

// Implements: REQ-NOTIF-002.
// Per: ADR-0017.
// Discipline: C-14.
// admin.go owns the admin shell wiring for notification_management: the
// sidebar section, the entity-CRUD registration, and a server-rendered page
// stub that hosts the notifications dashboard.
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
const adminModuleID = "notification_management"

// AdminPagePath is the canonical path under which the notification admin
// page is mounted by registerAdmin.
const AdminPagePath = "/admin/notifications"

var dashboardTemplate = template.Must(template.New("notification-admin").Parse(`
<!doctype html>
<html lang="en">
<head><meta charset="utf-8"><title>{{.Title}}</title></head>
<body>
<h1>{{.Title}}</h1>
<p>{{.Description}}</p>
<p>Notifications API: <code>{{.APIPath}}</code></p>
<p>Subscriptions API: <code>{{.SubscriptionAPIPath}}</code></p>
</body>
</html>
`))

type dashboardData struct {
	Title               string
	Description         string
	APIPath             string
	SubscriptionAPIPath string
}

func renderAdminDashboard() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		err := dashboardTemplate.Execute(w, dashboardData{
			Title:               "Notifications",
			Description:         "In-app notifications and channel subscriptions.",
			APIPath:             html.EscapeString(APIPath),
			SubscriptionAPIPath: html.EscapeString(SubscriptionAPIPath),
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
		return fmt.Errorf("notification: admin entity CRUD: %w", err)
	}
	if err := r.RegisterPage(portslib.AdminPage{
		ModuleID: adminModuleID,
		Path:     AdminPagePath,
		Title:    "Notifications",
		Render:   renderAdminDashboard(),
	}); err != nil {
		return fmt.Errorf("notification: admin page: %w", err)
	}
	if err := r.RegisterSidebarSection(portslib.SidebarSection{
		ModuleID: adminModuleID,
		Label:    "Notifications",
		Order:    50,
		Items: []portslib.SidebarItem{
			{Path: AdminPagePath, Label: "Notification log"},
		},
	}); err != nil {
		return fmt.Errorf("notification: admin sidebar: %w", err)
	}
	return nil
}
