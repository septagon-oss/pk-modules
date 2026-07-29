// Implements: REQ-004.
// Per: ADR-0017, ADR-0022, ADR-0029, ADR-0031.
// Discipline: C-14.

package admin

// view_list.go renders the resource list page as ONE pk-ui organism: the
// DataGrid composes the search bar, refresh action, sortable table, and
// cursor pagination; the page slots its live status line and the two empty
// panels into the organism's children seam. The DOM is a contract with
// _admin.js — ids, data-* hooks, and ARIA relations are what the script
// binds — but every component's markup and classes are pk-ui's own. The
// page head keeps its editorial classes per the chrome line drawn in
// view_chrome.go.

import (
	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"

	"github.com/septagon-oss/pk-ui/contracts/atoms"
	"github.com/septagon-oss/pk-ui/contracts/molecules"
	"github.com/septagon-oss/pk-ui/contracts/organisms"
	"github.com/septagon-oss/pk-ui/render/web"
)

func entityListView(data entityListData) g.Node {
	resource := data.Resource
	listPath := data.BasePath + "/" + resource.ModuleID + "/" + resource.EntityName
	hasActions := resource.CanEdit || resource.CanDelete || len(resource.Actions) > 0

	// Every API-backed column sorts client-side within the loaded page —
	// the list endpoint pages by limit/offset and reports no total, so
	// page-scoped sort matches the page-scoped search beside it.
	columns := make([]molecules.TableColumn, 0, len(resource.Columns)+1)
	for _, column := range resource.Columns {
		columns = append(columns, molecules.TableColumn{
			Key:      column.Key,
			Label:    column.Label,
			Sortable: true,
			Primary:  column.Primary,
		})
	}
	if hasActions {
		columns = append(columns, molecules.TableColumn{Key: "__actions", Label: "Actions"})
	}

	return layout(data.shellView, h.Section(
		h.Class("pk-resource-page"),
		g.Attr("data-pk-page", "resource-list"),
		g.Attr("data-resource-config", data.ResourceConfig),
		g.Attr("data-list-path", listPath),

		h.Header(h.Class("pk-page-head"),
			h.Div(
				h.A(h.Class("pk-backlink"), h.Href(data.BasePath), g.Text("← Overview")),
				h.P(h.Class("pk-eyebrow"), g.Text(resource.ModuleID)),
				h.H1(g.Text(resource.PluralLabel)),
				g.If(resource.Description != "", h.P(h.Class("pk-lede"), g.Text(resource.Description))),
			),
			g.If(resource.CanCreate,
				h.A(h.Class(web.ButtonClasses("primary", "medium").Compile()), h.Href(listPath+"/new"),
					g.Text("New "+resource.SingularLabel)),
			),
		),

		web.DataGrid(organisms.DataGridProps{
			Search: molecules.SearchBarProps{
				ComponentProps: contractProps("pk-resource-search", nil),
				Label:          "Search " + resource.PluralLabel,
				Placeholder:    "Filter this page…",
			},
			Actions: []atoms.ButtonProps{{
				ComponentProps: contractProps("pk-resource-refresh", nil),
				Label:          "Refresh", Variant: "secondary", Tone: "neutral",
			}},
			Table: molecules.TableProps{
				ComponentProps: contractProps("pk-resource-table", map[string]string{
					"tabindex":   "0",
					"aria-label": resource.PluralLabel + " table",
				}),
				Sortable:  true,
				Columns:   columns,
				EmptyText: "Loading " + resource.PluralLabel + "…",
			},
			Pagination: molecules.PaginationProps{
				ComponentProps: contractProps("pk-resource-pagination", nil),
				CurrentPage:    1,
			},
		},
			h.Div(h.Class(web.TextClasses("muted", "sm", "").Compile()+" pk-inline-status"),
				h.ID("pk-resource-status"), h.Role("status"), g.Attr("aria-live", "polite"),
				g.Text("Loading "+resource.PluralLabel+"…")),
			web.EmptyState(atoms.EmptyStateProps{
				ComponentProps: hiddenProps("pk-resource-empty"),
				Bordered:       true,
				Title:          "No " + resource.PluralLabel + " found",
				Description:    "There is nothing to show on this page yet.",
			}),
			web.EmptyState(atoms.EmptyStateProps{
				ComponentProps: hiddenProps("pk-resource-nomatch"),
				Bordered:       true, Compact: true,
				Title:       "No matches",
				Description: "No records on this page match your filter.",
			}),
		),

		h.NoScript(h.P(h.Class(web.TextClasses("danger", "sm", "").Compile()),
			g.Text("JavaScript is required for live resource management."))),
	))
}
