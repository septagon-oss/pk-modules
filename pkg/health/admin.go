// Implements: REQ-HEALTH-001.
// Per: ADR-0017.
// Discipline: C-14.

package health

import (
	"fmt"

	"github.com/septagon-oss/pk-modules/pkg/portslib"
)

const (
	adminModuleID = "health_management"
	AdminPagePath = "/admin/health_management/Health"
)

func registerAdmin(registrar portslib.AdminRegistrar, _ HealthService) error {
	if registrar == nil {
		return nil
	}
	if err := registrar.RegisterResource(portslib.AdminResource{
		ModuleID:      adminModuleID,
		EntityName:    "Health",
		SingularLabel: "health report",
		PluralLabel:   "System health",
		Description:   "Live reachability checks reported by every composed data module.",
		APIPath:       APIPath,
		Columns: []portslib.AdminColumn{
			{Key: "status", Label: "Overall status", Kind: portslib.AdminFieldStatus, Primary: true},
			{Key: "checks", Label: "Registered checks", Kind: portslib.AdminFieldCount},
		},
	}); err != nil {
		return fmt.Errorf("health: admin resource: %w", err)
	}
	return registrar.RegisterSidebarSection(portslib.SidebarSection{
		ModuleID: adminModuleID,
		Label:    "Operations",
		Order:    80,
		Items:    []portslib.SidebarItem{{Path: AdminPagePath, Label: "System health"}},
	})
}
