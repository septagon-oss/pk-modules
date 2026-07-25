// Implements: REQ-004.
// Per: ADR-0017, ADR-0022, ADR-0029, ADR-0031.
// Discipline: C-14.

package admin

// view_form.go renders the create/edit page for a schema-aware resource.
// Field markup follows the AdminField contract exactly as the retired
// template did — same ids, names, validation attributes, and help wiring —
// because _admin.js serializes the form by those names and the module's API
// validates by them. Styling is tw class lists; a native <select> keeps an
// input-matched look here until pk-ui grows a Select contract, which is the
// one seam this file styles without a shared component.

import (
	"strconv"

	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"

	"github.com/septagon-oss/pk-modules/pkg/portslib"
	"github.com/septagon-oss/tw"
)

// formClasses are the component styles this page composes; served via
// viewClassLists like every other view-declared list.
var formClasses = struct {
	Form      tw.ClassList
	Status    tw.ClassList
	Fieldset  tw.ClassList
	Legend    tw.ClassList
	Grid      tw.ClassList
	Field     tw.ClassList
	FieldWide tw.ClassList
	Label     tw.ClassList
	Required  tw.ClassList
	Help      tw.ClassList
	Input     tw.ClassList
	CheckRow  tw.ClassList
	Checkbox  tw.ClassList
	Actions   tw.ClassList
	Secret    tw.ClassList
	SecretRow tw.ClassList
	Code      tw.ClassList
}{
	Form: tw.New().Display(tw.DisplayFlex).FlexDir(tw.FlexCol).Gap(tw.S6).MarginY(tw.S4),
	Status: tw.New().Rounded(tw.RadiusMD).Border(tw.Border1).BorderColor(tw.BorderDanger).
		Bg(tw.SurfaceDangerSoft).TextColor(tw.FgDanger).PaddingX(tw.S4).PaddingY(tw.S3).FontSize(tw.TextSM),
	Fieldset: tw.New().Border(tw.Border1).BorderColor(tw.BorderPrimary).Rounded(tw.RadiusLG).
		Bg(tw.SurfacePrimary).Padding(tw.S6),
	Legend: tw.New().FontFamily(tw.FontSerif).FontSize(tw.TextLG).FontWeight(tw.FontSemibold).
		TextColor(tw.FgPrimary).PaddingX(tw.S2),
	Grid: tw.New().Display(tw.DisplayGrid).GridCols(1).Gap(tw.S5).MarginY(tw.S2).
		Breakpoint(tw.BreakpointMD, func(c tw.ClassList) tw.ClassList { return c.GridCols(2) }),
	Field:     tw.New().Display(tw.DisplayFlex).FlexDir(tw.FlexCol).Gap(tw.S1_5),
	FieldWide: tw.New().ColSpanFull(),
	Label:     tw.New().FontSize(tw.TextSM).FontWeight(tw.FontMedium).TextColor(tw.FgPrimary),
	Required:  tw.New().TextColor(tw.FgDanger),
	Help:      tw.New().FontSize(tw.TextXS).TextColor(tw.FgMuted),
	Input: tw.New().Display(tw.DisplayBlock).Width(tw.SFull).
		Rounded(tw.RadiusMD).Border(tw.Border1).BorderColor(tw.BorderPrimary).
		Bg(tw.SurfacePrimary).TextColor(tw.FgPrimary).
		PaddingX(tw.S3).PaddingY(tw.S2).FontSize(tw.TextSM).
		On(tw.StatePlaceholder, func(c tw.ClassList) tw.ClassList { return c.TextColor(tw.FgPlaceholder) }).
		On(tw.StateDisabled, func(c tw.ClassList) tw.ClassList {
			return c.Bg(tw.SurfaceDisabled).Cursor(tw.CursorNotAllowed)
		}).
		On(tw.StateFocusVisible, func(c tw.ClassList) tw.ClassList {
			return c.Ring(tw.Ring2).RingColor(tw.RingFocus).RingOffset(tw.RingOffset1)
		}),
	CheckRow: tw.New().Display(tw.DisplayInlineFlex).Items(tw.ItemsStart).Gap(tw.S2),
	Checkbox: tw.New().Width(tw.S4).Height(tw.S4).Rounded(tw.RadiusSM).
		Border(tw.Border1).BorderColor(tw.BorderPrimary).Cursor(tw.CursorPointer),
	Actions: tw.New().Display(tw.DisplayFlex).Items(tw.ItemsCenter).Gap(tw.S3),
	Secret: tw.New().Rounded(tw.RadiusLG).Border(tw.Border1).BorderColor(tw.BorderBrand).
		Bg(tw.SurfaceBrandSoft).Padding(tw.S6).Display(tw.DisplayFlex).FlexDir(tw.FlexCol).Gap(tw.S3),
	SecretRow: tw.New().Display(tw.DisplayFlex).Items(tw.ItemsCenter).Gap(tw.S3).FlexWrap(),
	Code: tw.New().FontFamily(tw.FontMono).FontSize(tw.TextSM).Bg(tw.SurfacePrimary).
		Rounded(tw.RadiusMD).Border(tw.Border1).BorderColor(tw.BorderPrimary).
		PaddingX(tw.S3).PaddingY(tw.S2).BreakAll(),
}

func entityFormView(data entityFormData) g.Node {
	resource := data.Resource
	listPath := data.BasePath + "/" + resource.ModuleID + "/" + resource.EntityName
	title := "New " + resource.SingularLabel
	submit := "Create " + resource.SingularLabel
	if data.EntityID != "" {
		title = "Edit " + resource.SingularLabel
		submit = "Save changes"
	}

	fields := make([]g.Node, 0, len(resource.Fields))
	for _, field := range resource.Fields {
		fields = append(fields, formField(field, data.EntityID == ""))
	}

	return layout(data.shellView, h.Section(
		h.Class("pk-form-page"),
		g.Attr("data-pk-page", "resource-form"),
		g.Attr("data-resource-config", data.ResourceConfig),
		g.Attr("data-resource-id", data.EntityID),
		g.Attr("data-list-path", listPath),

		h.Header(h.Class("pk-page-head"),
			h.Div(
				h.A(h.Class("pk-backlink"), h.Href(listPath), g.Text("← "+resource.PluralLabel)),
				h.P(h.Class("pk-eyebrow"), g.Text(resource.ModuleID)),
				h.H1(g.Text(title)),
				g.If(resource.Description != "", h.P(h.Class("pk-lede"), g.Text(resource.Description))),
			),
		),

		h.Form(h.Class(formClasses.Form.Compile()), h.ID("pk-resource-form"), g.Attr("novalidate"),
			h.Div(h.Class(formClasses.Status.Compile()), h.ID("pk-form-status"),
				h.Role("alert"), g.Attr("aria-live", "assertive"), g.Attr("hidden")),
			h.FieldSet(h.Class(formClasses.Fieldset.Compile()),
				h.Legend(h.Class(formClasses.Legend.Compile()), g.Text("Details")),
				h.Div(append([]g.Node{h.Class(formClasses.Grid.Compile())}, fields...)...),
			),
			h.Div(h.Class(formClasses.Actions.Compile()),
				h.Button(h.Class(listClasses.BtnPrimary.Compile()),
					h.ID("pk-form-submit"), h.Type("submit"), g.Text(submit)),
				h.A(h.Class(listClasses.Btn.Compile()), h.Href(listPath), g.Text("Cancel")),
			),
		),

		h.Section(h.Class(formClasses.Secret.Compile()), h.ID("pk-secret-panel"),
			g.Attr("aria-live", "polite"), g.Attr("hidden"),
			h.P(h.Class("pk-eyebrow"), g.Text("Copy this value now")),
			h.H2(h.Class(formClasses.Legend.Compile()), g.Text("Created successfully")),
			h.P(h.Class(formClasses.Help.Compile()), g.Text("This secret is shown once. Store it somewhere safe before leaving.")),
			h.Div(h.Class(formClasses.SecretRow.Compile()),
				h.Code(h.Class(formClasses.Code.Compile()), h.ID("pk-secret-value")),
				h.Button(h.Class(listClasses.Btn.Compile()), h.ID("pk-secret-copy"), h.Type("button"), g.Text("Copy")),
			),
			h.A(h.Class(listClasses.BtnPrimary.Compile()), h.Href(listPath),
				g.Text("Return to "+resource.PluralLabel)),
		),
	))
}

// formField renders one AdminField per its kind, preserving the exact
// id/name/validation attribute contract _admin.js and the API rely on.
func formField(field portslib.AdminField, creating bool) g.Node {
	id := "field-" + field.Key
	required := field.Required || (creating && field.RequiredOnCreate)

	if field.Kind == portslib.AdminFieldBoolean {
		return h.Label(h.Class(formClasses.CheckRow.Compile()), h.For(id),
			h.Input(h.Class(formClasses.Checkbox.Compile()), h.ID(id), h.Name(field.Key), h.Type("checkbox"),
				g.If(field.ReadOnly, h.Disabled())),
			h.Span(h.Class(formClasses.Field.Compile()),
				h.Strong(h.Class(formClasses.Label.Compile()), g.Text(field.Label)),
				g.If(field.Help != "", h.Small(h.Class(formClasses.Help.Compile()), g.Text(field.Help))),
			),
		)
	}

	label := h.Span(h.Class(formClasses.Label.Compile()), g.Text(field.Label),
		g.If(required, g.Group([]g.Node{
			h.B(h.Class(formClasses.Required.Compile()), g.Attr("aria-hidden", "true"), g.Text(" *")),
			h.Span(h.Class("pk-visually-hidden"), g.Text(" required")),
		})),
	)
	var control g.Node
	switch field.Kind {
	case portslib.AdminFieldTextarea:
		control = h.Textarea(h.Class(formClasses.Input.Compile()), h.ID(id), h.Name(field.Key), h.Rows("8"),
			g.If(field.Placeholder != "", h.Placeholder(field.Placeholder)),
			g.If(required, h.Required()),
			g.If(field.ReadOnly, h.ReadOnly()),
		)
	case portslib.AdminFieldSelect:
		options := make([]g.Node, 0, len(field.Options)+1)
		if !field.Required {
			options = append(options, h.Option(h.Value(""), g.Text("Choose…")))
		}
		for _, option := range field.Options {
			options = append(options, h.Option(h.Value(option.Value), g.Text(option.Label)))
		}
		selectNodes := []g.Node{h.Class(formClasses.Input.Compile()), h.ID(id), h.Name(field.Key)}
		if required {
			selectNodes = append(selectNodes, h.Required())
		}
		if field.ReadOnly {
			selectNodes = append(selectNodes, h.Disabled())
		}
		control = h.Select(append(selectNodes, options...)...)
	default:
		control = h.Input(append(inputTypeAttrs(field), fieldConstraintAttrs(field, required)...)...)
	}

	wrap := formClasses.Field
	if field.Kind == portslib.AdminFieldTextarea {
		wrap = wrap.Merge(formClasses.FieldWide)
	}
	return h.Label(h.Class(wrap.Compile()), h.For(id),
		label,
		control,
		g.If(field.Help != "", h.Small(h.ID(id+"-help"), h.Class(formClasses.Help.Compile()), g.Text(field.Help))),
	)
}

// inputTypeAttrs maps an AdminField kind onto the input element's type and
// kind-specific attributes.
func inputTypeAttrs(field portslib.AdminField) []g.Node {
	id := "field-" + field.Key
	nodes := []g.Node{h.Class(formClasses.Input.Compile()), h.ID(id), h.Name(field.Key)}
	switch field.Kind {
	case portslib.AdminFieldEmail:
		nodes = append(nodes, h.Type("email"))
	case portslib.AdminFieldPassword:
		nodes = append(nodes, h.Type("password"))
	case portslib.AdminFieldNumber:
		nodes = append(nodes, h.Type("number"))
	case portslib.AdminFieldDateTime:
		nodes = append(nodes, h.Type("datetime-local"))
	case portslib.AdminFieldSlug:
		nodes = append(nodes, h.Type("text"), h.Pattern("[a-z0-9]+(?:-[a-z0-9]+)*"),
			g.Attr("autocapitalize", "none"), g.Attr("spellcheck", "false"))
	default:
		nodes = append(nodes, h.Type("text"))
	}
	return nodes
}

// fieldConstraintAttrs renders placeholder, required/readonly, and the
// kind-dependent min/max semantics: numeric bounds for numbers, minlength
// plus the UTF-8 byte cap for passwords, and character lengths otherwise.
func fieldConstraintAttrs(field portslib.AdminField, required bool) []g.Node {
	var nodes []g.Node
	if field.Placeholder != "" {
		nodes = append(nodes, h.Placeholder(field.Placeholder))
	}
	if required {
		nodes = append(nodes, h.Required())
	}
	if field.ReadOnly {
		nodes = append(nodes, h.ReadOnly())
	}
	min, max := itoaNonZero(field.Min), itoaNonZero(field.Max)
	switch field.Kind {
	case portslib.AdminFieldNumber:
		if min != "" {
			nodes = append(nodes, h.Min(min))
		}
		if max != "" {
			nodes = append(nodes, h.Max(max))
		}
	case portslib.AdminFieldPassword:
		if min != "" {
			nodes = append(nodes, h.MinLength(min))
		}
		if max != "" {
			nodes = append(nodes, g.Attr("data-max-utf8-bytes", max))
		}
	default:
		if min != "" {
			nodes = append(nodes, h.MinLength(min))
		}
		if max != "" {
			nodes = append(nodes, h.MaxLength(max))
		}
	}
	return nodes
}

// itoaNonZero renders a positive constraint bound; zero means unset in the
// AdminField contract and emits nothing.
func itoaNonZero(n int) string {
	if n <= 0 {
		return ""
	}
	return strconv.Itoa(n)
}
