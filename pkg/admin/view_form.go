// Implements: REQ-004.
// Per: ADR-0017, ADR-0022, ADR-0029, ADR-0031.
// Discipline: C-14.

package admin

// view_form.go renders the create/edit page for a schema-aware resource by
// composing pk-ui form components. The AdminField contract carries over
// exactly — ids ("field-<key>"), names, validation attributes, and help
// wiring are what _admin.js serializes and the module's API validates — but
// every control is pk-ui's: Input, Textarea, Select, Checkbox, Alert,
// Button. Kind-specific constraint attributes ride ComponentProps.Attrs,
// which the renderers place on the control element.

import (
	"strconv"

	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"

	"github.com/septagon-oss/pk-modules/pkg/portslib"
	"github.com/septagon-oss/pk-ui/contracts/atoms"
	"github.com/septagon-oss/pk-ui/render/web"
)

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

		h.Form(h.Class(layoutClasses.FormStack.Compile()), h.ID("pk-resource-form"), g.Attr("novalidate"),
			web.Alert(atoms.AlertProps{
				ComponentProps: hiddenProps("pk-form-status"),
				Variant:        "error",
			}),
			h.FieldSet(h.Class(web.CardClasses().Compile()),
				h.Legend(web.Heading(atoms.HeadingProps{Level: 4, Text: "Details"})),
				h.Div(append([]g.Node{h.Class(layoutClasses.FormGrid.Compile())}, fields...)...),
			),
			h.Div(h.Class(layoutClasses.ActionsRow.Compile()),
				web.Button(atoms.ButtonProps{
					ComponentProps: contractProps("pk-form-submit", nil),
					Text:           submit, Variant: "primary", Type: "submit",
				}),
				h.A(h.Class(web.ButtonClasses("secondary", "medium").Compile()), h.Href(listPath), g.Text("Cancel")),
			),
		),

		h.Section(h.Class(layoutClasses.SecretStack.Compile()), h.ID("pk-secret-panel"),
			g.Attr("aria-live", "polite"), g.Attr("hidden"),
			web.Alert(atoms.AlertProps{
				Variant: "success",
				Title:   "Created successfully",
				Message: "This secret is shown once. Store it somewhere safe before leaving.",
			}),
			h.Div(h.Class(layoutClasses.SecretRow.Compile()),
				h.Code(h.Class(web.InlineCodeClasses().Compile()), h.ID("pk-secret-value")),
				web.Button(atoms.ButtonProps{
					ComponentProps: contractProps("pk-secret-copy", nil),
					Text:           "Copy", Variant: "secondary",
				}),
			),
			h.Div(
				h.A(h.Class(web.ButtonClasses("primary", "medium").Compile()), h.Href(listPath),
					g.Text("Return to "+resource.PluralLabel)),
			),
		),
	))
}

// formField renders one AdminField through the pk-ui control matching its
// kind, preserving the exact id/name/validation attribute contract
// _admin.js and the API rely on.
func formField(field portslib.AdminField, creating bool) g.Node {
	id := "field-" + field.Key
	required := field.Required || (creating && field.RequiredOnCreate)

	switch field.Kind {
	case portslib.AdminFieldBoolean:
		return web.Checkbox(atoms.CheckboxProps{
			ComponentProps: contractProps(id, nil),
			Name:           field.Key,
			Label:          field.Label,
			HelpText:       field.Help,
			Required:       required,
		})
	case portslib.AdminFieldTextarea:
		return h.Div(h.Class(layoutClasses.FieldWide.Compile()),
			web.Textarea(atoms.TextareaProps{
				ComponentProps: contractProps(id, constraintAttrs(field)),
				Name:           field.Key,
				Label:          field.Label,
				Placeholder:    field.Placeholder,
				Required:       required,
				ReadOnly:       field.ReadOnly,
				Rows:           8,
				HelperText:     field.Help,
			}),
		)
	case portslib.AdminFieldSelect:
		options := make([]atoms.SelectOption, 0, len(field.Options))
		for _, option := range field.Options {
			options = append(options, atoms.SelectOption{Label: option.Label, Value: option.Value})
		}
		return web.Select(atoms.SelectProps{
			ComponentProps: contractProps(id, constraintAttrs(field)),
			Name:           field.Key,
			Label:          field.Label,
			Options:        options,
			Required:       field.Required,
			HelpText:       field.Help,
		})
	default:
		return web.Input(atoms.InputProps{
			ComponentProps: contractProps(id, constraintAttrs(field)),
			Name:           field.Key,
			Type:           inputType(field.Kind),
			Label:          field.Label,
			Placeholder:    field.Placeholder,
			Required:       required,
			ReadOnly:       field.ReadOnly,
			Pattern:        slugPattern(field.Kind),
			Min:            numericBound(field.Kind, field.Min),
			Max:            numericBound(field.Kind, field.Max),
			HelpText:       field.Help,
		})
	}
}

// inputType maps an AdminField kind onto the input element's type.
func inputType(kind portslib.AdminFieldKind) string {
	switch kind {
	case portslib.AdminFieldEmail:
		return "email"
	case portslib.AdminFieldPassword:
		return "password"
	case portslib.AdminFieldNumber:
		return "number"
	case portslib.AdminFieldDateTime:
		return "datetime-local"
	default:
		return "text"
	}
}

// slugPattern returns the slug kind's pattern; other kinds carry none.
func slugPattern(kind portslib.AdminFieldKind) string {
	if kind == portslib.AdminFieldSlug {
		return "[a-z0-9]+(?:-[a-z0-9]+)*"
	}
	return ""
}

// numericBound renders Min/Max as the input's numeric bounds — only the
// number kind uses min/max attributes; other kinds express length limits
// through constraintAttrs.
func numericBound(kind portslib.AdminFieldKind, n int) string {
	if kind != portslib.AdminFieldNumber {
		return ""
	}
	return itoaNonZero(n)
}

// constraintAttrs carries the kind-dependent contract attributes the pk-ui
// renderers place on the control element: minlength plus the UTF-8 byte cap
// for passwords, character lengths for text kinds, and the slug kind's
// keyboard hints.
func constraintAttrs(field portslib.AdminField) map[string]string {
	attrs := map[string]string{}
	min, max := itoaNonZero(field.Min), itoaNonZero(field.Max)
	switch field.Kind {
	case portslib.AdminFieldNumber:
		// bounds ride InputProps.Min/Max
	case portslib.AdminFieldPassword:
		if min != "" {
			attrs["minlength"] = min
		}
		if max != "" {
			attrs["data-max-utf8-bytes"] = max
		}
	default:
		if min != "" {
			attrs["minlength"] = min
		}
		if max != "" {
			attrs["maxlength"] = max
		}
	}
	if field.Kind == portslib.AdminFieldSlug {
		attrs["autocapitalize"] = "none"
		attrs["spellcheck"] = "false"
	}
	if len(attrs) == 0 {
		return nil
	}
	return attrs
}

// itoaNonZero renders a positive constraint bound; zero means unset in the
// AdminField contract and emits nothing.
func itoaNonZero(n int) string {
	if n <= 0 {
		return ""
	}
	return strconv.Itoa(n)
}
