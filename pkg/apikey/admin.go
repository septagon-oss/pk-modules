// Implements: REQ-APIKEY-001.
// Per: ADR-0017.
// Discipline: C-14.

package apikey

import (
	"fmt"

	"github.com/septagon-oss/pk-modules/pkg/portslib"
)

const (
	adminModuleID = "api_key_management"
	AdminPagePath = "/admin/api_key_management/APIKey"
)

func registerAdmin(registrar portslib.AdminRegistrar) error {
	if registrar == nil {
		return nil
	}
	if err := registrar.RegisterResource(portslib.AdminResource{
		ModuleID:      adminModuleID,
		EntityName:    EntityName,
		SingularLabel: "API key",
		PluralLabel:   "API keys",
		Description:   "Issue narrowly scoped credentials for automation and integrations.",
		APIPath:       APIPath,
		CanCreate:     true,
		CanDelete:     true,
		SuccessField:  "plaintext",
		Columns: []portslib.AdminColumn{
			{Key: "name", Label: "Name", Primary: true},
			{Key: "prefix", Label: "Prefix"},
			{Key: "scopes", Label: "Scopes", Kind: portslib.AdminFieldTags},
			{Key: "last_used_at", Label: "Last used", Kind: portslib.AdminFieldDateTime},
			{Key: "expires_at", Label: "Expires", Kind: portslib.AdminFieldDateTime},
		},
		Fields: []portslib.AdminField{
			{Key: "name", Label: "Name", Required: true, Placeholder: "Production sync", Help: "A recognizable name for future revocation."},
			{Key: "scopes", Label: "Scopes", Kind: portslib.AdminFieldTags, Required: true, Placeholder: "resource:read, resource:write", Help: "Comma-separated capabilities. Console and admin scopes are reserved."},
			{Key: "ttl_seconds", Label: "Lifetime in seconds", Kind: portslib.AdminFieldNumber, Min: 60, Placeholder: "2592000", Help: "Leave empty for no automatic expiry."},
		},
	}); err != nil {
		return fmt.Errorf("apikey: admin resource: %w", err)
	}
	return registrar.RegisterSidebarSection(portslib.SidebarSection{
		ModuleID: adminModuleID,
		Label:    "Access",
		Order:    30,
		Items:    []portslib.SidebarItem{{Path: AdminPagePath, Label: "API keys"}},
	})
}
