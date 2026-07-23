// Implements: REQ-CONTENT-001.
// Per: ADR-0017.
// Discipline: C-14.

package content

import (
	"fmt"

	"github.com/septagon-oss/pk-modules/pkg/portslib"
)

const (
	adminModuleID = "content_management"
	AdminPagePath = "/admin/content_management/Content"
)

func registerAdmin(registrar portslib.AdminRegistrar) error {
	if registrar == nil {
		return nil
	}
	if err := registrar.RegisterResource(portslib.AdminResource{
		ModuleID:      adminModuleID,
		EntityName:    EntityName,
		SingularLabel: "content item",
		PluralLabel:   "Content",
		Description:   "Author tenant-owned pages, posts, and reusable snippets.",
		APIPath:       APIPath,
		CanCreate:     true,
		CanEdit:       true,
		CanDelete:     true,
		Columns: []portslib.AdminColumn{
			{Key: "title", Label: "Title", Primary: true},
			{Key: "kind", Label: "Kind", Kind: portslib.AdminFieldStatus},
			{Key: "slug", Label: "Slug"},
			{Key: "published_at", Label: "Published", Kind: portslib.AdminFieldDateTime},
			{Key: "updated_at", Label: "Updated", Kind: portslib.AdminFieldDateTime},
		},
		Fields: []portslib.AdminField{
			{Key: "title", Label: "Title", Required: true, Placeholder: "Introducing our spring release"},
			{Key: "slug", Label: "Slug", Kind: portslib.AdminFieldSlug, Required: true, Placeholder: "spring-release"},
			{Key: "kind", Label: "Kind", Kind: portslib.AdminFieldSelect, Required: true, DefaultValue: KindPage, Options: []portslib.AdminOption{
				{Value: KindPage, Label: "Page"},
				{Value: KindPost, Label: "Post"},
				{Value: KindSnippet, Label: "Snippet"},
			}},
			{Key: "body_format", Label: "Body format", Kind: portslib.AdminFieldSelect, Required: true, DefaultValue: BodyFormatMarkdown, Options: []portslib.AdminOption{
				{Value: BodyFormatMarkdown, Label: "Markdown"},
				{Value: BodyFormatHTML, Label: "HTML"},
				{Value: BodyFormatText, Label: "Plain text"},
			}},
			{Key: "body", Label: "Body", Kind: portslib.AdminFieldTextarea, Required: true, Placeholder: "Write the content body…"},
		},
		Actions: []portslib.AdminAction{
			{Label: "Publish", PathSuffix: "/publish"},
			{Label: "Unpublish", PathSuffix: "/unpublish", Confirm: "Unpublish this content item?"},
		},
	}); err != nil {
		return fmt.Errorf("content: admin resource: %w", err)
	}
	return registrar.RegisterSidebarSection(portslib.SidebarSection{
		ModuleID: adminModuleID,
		Label:    "Publishing",
		Order:    40,
		Items:    []portslib.SidebarItem{{Path: AdminPagePath, Label: "Content"}},
	})
}
