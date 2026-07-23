// Implements: REQ-TENANT-001.
// Per: ADR-0017.
// Discipline: C-14.

package tenant

import (
	"fmt"

	"github.com/septagon-oss/pk-modules/pkg/portslib"
)

const (
	adminModuleID = "tenant_management"
	AdminPagePath = "/admin/tenant_management/Tenant"
)

func registerAdmin(registrar portslib.AdminRegistrar) error {
	if registrar == nil {
		return nil
	}
	if err := registrar.RegisterResource(portslib.AdminResource{
		ModuleID:      adminModuleID,
		EntityName:    EntityName,
		SingularLabel: "tenant",
		PluralLabel:   "Tenants",
		Description:   "Review and update the organization boundary for this workspace.",
		APIPath:       APIPath,
		CanEdit:       true,
		Columns: []portslib.AdminColumn{
			{Key: "name", Label: "Name", Primary: true},
			{Key: "slug", Label: "Slug"},
			{Key: "updated_at", Label: "Last updated", Kind: portslib.AdminFieldDateTime},
		},
		Fields: []portslib.AdminField{
			{Key: "name", Label: "Name", Required: true, Help: "The organization name shown to operators."},
			{Key: "slug", Label: "Slug", Kind: portslib.AdminFieldSlug, Required: true, Help: "Lowercase letters, numbers, and hyphens."},
		},
	}); err != nil {
		return fmt.Errorf("tenant: admin resource: %w", err)
	}
	return registrar.RegisterSidebarSection(portslib.SidebarSection{
		ModuleID: adminModuleID,
		Label:    "Workspace",
		Order:    10,
		Items:    []portslib.SidebarItem{{Path: AdminPagePath, Label: "Tenant"}},
	})
}
