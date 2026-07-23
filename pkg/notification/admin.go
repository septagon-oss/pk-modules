// Implements: REQ-NOTIF-002.
// Per: ADR-0017.
// Discipline: C-14.

package notification

import (
	"fmt"

	"github.com/septagon-oss/pk-modules/pkg/portslib"
)

const (
	adminModuleID = "notification_management"
	AdminPagePath = "/admin/notification_management/Notification"
)

func registerAdmin(registrar portslib.AdminRegistrar) error {
	if registrar == nil {
		return nil
	}
	if err := registrar.RegisterResource(portslib.AdminResource{
		ModuleID:      adminModuleID,
		EntityName:    EntityName,
		SingularLabel: "notification",
		PluralLabel:   "Notifications",
		Description:   "Review and send in-app messages for the current operator account.",
		APIPath:       APIPath,
		CanCreate:     true,
		Columns: []portslib.AdminColumn{
			{Key: "title", Label: "Title", Primary: true},
			{Key: "category", Label: "Category"},
			{Key: "severity", Label: "Severity", Kind: portslib.AdminFieldStatus},
			{Key: "emitted_at", Label: "Sent", Kind: portslib.AdminFieldDateTime},
		},
		Fields: []portslib.AdminField{
			{Key: "title", Label: "Title", Required: true, Placeholder: "Deployment complete"},
			{Key: "category", Label: "Category", Required: true, Placeholder: "operations"},
			{Key: "severity", Label: "Severity", Kind: portslib.AdminFieldSelect, Required: true, DefaultValue: SeverityInfo, Options: []portslib.AdminOption{
				{Value: SeverityInfo, Label: "Information"},
				{Value: SeverityWarning, Label: "Warning"},
				{Value: SeverityCritical, Label: "Critical"},
			}},
			{Key: "body", Label: "Message", Kind: portslib.AdminFieldTextarea, Required: true, Placeholder: "Write a concise notification…"},
		},
		Actions: []portslib.AdminAction{{Label: "Mark read", PathSuffix: "/read"}},
	}); err != nil {
		return fmt.Errorf("notification: admin resource: %w", err)
	}
	return registrar.RegisterSidebarSection(portslib.SidebarSection{
		ModuleID: adminModuleID,
		Label:    "Operations",
		Order:    50,
		Items:    []portslib.SidebarItem{{Path: AdminPagePath, Label: "Notifications"}},
	})
}
