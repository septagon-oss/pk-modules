// Implements: REQ-USER-001.
// Per: ADR-0017.
// Discipline: C-14.

package user

import (
	"fmt"

	"github.com/septagon-oss/pk-modules/pkg/portslib"
)

const (
	adminModuleID = "user_management"
	AdminPagePath = "/admin/user_management/User"
)

func registerAdmin(registrar portslib.AdminRegistrar) error {
	if registrar == nil {
		return nil
	}
	if err := registrar.RegisterResource(portslib.AdminResource{
		ModuleID:      adminModuleID,
		EntityName:    EntityName,
		SingularLabel: "user",
		PluralLabel:   "Users",
		Description:   "Manage people, profiles, credentials, and access state within this tenant.",
		APIPath:       APIPath,
		CanCreate:     true,
		CanEdit:       true,
		CanDelete:     true,
		Columns: []portslib.AdminColumn{
			{Key: "display_name", Label: "Name", Primary: true},
			{Key: "email", Label: "Email"},
			{Key: "username", Label: "Username"},
			{Key: "active", Label: "Active", Kind: portslib.AdminFieldBoolean},
			{Key: "created_at", Label: "Created", Kind: portslib.AdminFieldDateTime},
		},
		Fields: []portslib.AdminField{
			{Key: "display_name", Label: "Display name", Required: true, Placeholder: "Ada Lovelace"},
			{Key: "email", Label: "Email", Kind: portslib.AdminFieldEmail, Required: true, Placeholder: "ada@example.com"},
			{Key: "username", Label: "Username", Required: true, Placeholder: "ada"},
			{Key: "password", Label: "Password", Kind: portslib.AdminFieldPassword, RequiredOnCreate: true, Help: "Required for a new user. Leave blank while editing to keep the current password.", Min: 12},
			{Key: "active", Label: "Active", Kind: portslib.AdminFieldBoolean, DefaultValue: "true", Help: "Inactive users cannot create new sessions."},
		},
	}); err != nil {
		return fmt.Errorf("user: admin resource: %w", err)
	}
	return registrar.RegisterSidebarSection(portslib.SidebarSection{
		ModuleID: adminModuleID,
		Label:    "Access",
		Order:    20,
		Items:    []portslib.SidebarItem{{Path: AdminPagePath, Label: "Users"}},
	})
}
