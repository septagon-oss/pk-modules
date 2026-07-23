// Implements: REQ-AUDIT-001.
// Per: ADR-0017.
// Discipline: C-14.

package audit

import (
	"fmt"

	"github.com/septagon-oss/pk-modules/pkg/portslib"
)

const (
	adminModuleID = "audit_management"
	AdminPagePath = "/admin/audit_management/AuditEvent"
)

func registerAdmin(registrar portslib.AdminRegistrar) error {
	if registrar == nil {
		return nil
	}
	if err := registrar.RegisterResource(portslib.AdminResource{
		ModuleID:      adminModuleID,
		EntityName:    EntityName,
		SingularLabel: "audit event",
		PluralLabel:   "Audit log",
		Description:   "Immutable evidence of security-sensitive and lifecycle operations.",
		APIPath:       APIPath,
		Columns: []portslib.AdminColumn{
			{Key: "emitted_at", Label: "Time", Kind: portslib.AdminFieldDateTime, Primary: true},
			{Key: "actor", Label: "Actor"},
			{Key: "action", Label: "Action"},
			{Key: "resource", Label: "Resource"},
			{Key: "severity", Label: "Severity", Kind: portslib.AdminFieldStatus},
		},
	}); err != nil {
		return fmt.Errorf("audit: admin resource: %w", err)
	}
	return registrar.RegisterSidebarSection(portslib.SidebarSection{
		ModuleID: adminModuleID,
		Label:    "Operations",
		Order:    90,
		Items:    []portslib.SidebarItem{{Path: AdminPagePath, Label: "Audit log"}},
	})
}
