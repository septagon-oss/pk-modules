// Package overlay renders client-owned homepage overlay templates.
package overlay

// Implements: REQ-SITE-001.
// Per: ADR-0032.
// Discipline: C-14.
// overlay.go owns safe HTML rendering helpers for client-owned homepage
// overlays.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-10 (shared builders return errors), C-14 (every Go file declares its purpose).

import (
	"bytes"
	"fmt"
	"html/template"
	"net/mail"
	"net/url"
	"path"
	"reflect"
	"regexp"
	"strings"
)

// safeClientSlug matches a single safe path segment used in asset URLs: letters,
// digits, hyphen, and underscore only. It rejects ".", "..", path separators,
// and percent-encoded traversal so a slug can never escape PublicAssetBasePath.
var safeClientSlug = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// looksProtocolRelative reports whether a link would be treated by a browser as
// protocol-relative (and therefore external). Browsers normalize backslashes to
// forward slashes, so "/\\host", "\\\\host", and "//host" are all equivalent.
func looksProtocolRelative(link string) bool {
	return strings.HasPrefix(strings.ReplaceAll(link, `\`, "/"), "//")
}

// PublicAssetBasePath is the URL path prefix under which client overlay assets
// are served.
const PublicAssetBasePath = "/assets/overlays"

// RenderInput holds the template source, view data, asset base path, and extra
// template functions consumed by RenderFragment.
type RenderInput struct {
	TemplateSource string
	View           any
	AssetBase      string
	Funcs          template.FuncMap
}

// RenderFragment parses and executes the overlay homepage template in input and
// returns the rendered HTML fragment.
func RenderFragment(input RenderInput) (string, error) {
	tmpl, err := template.New("overlay-homepage").Funcs(FuncMap(input.AssetBase, input.Funcs)).Parse(input.TemplateSource)
	if err != nil {
		return "", fmt.Errorf("parse overlay homepage template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, input.View); err != nil {
		return "", fmt.Errorf("render overlay homepage template: %w", err)
	}
	return buf.String(), nil
}

// FuncMap returns the template.FuncMap of overlay rendering helpers (asset,
// link, external, mailto, tel, price, nonEmpty, dict, and arithmetic helpers),
// merged with any non-empty, non-nil functions from extra.
func FuncMap(assetBase string, extra template.FuncMap) template.FuncMap {
	funcs := template.FuncMap{
		"asset": func(assetPath string) string {
			assetPath = strings.TrimSpace(assetPath)
			if assetPath == "" {
				return ""
			}
			// Reject protocol-relative URLs ("//host/x", and backslash variants
			// like "/\\host" that browsers normalize) so they cannot smuggle in
			// an external origin disguised as a local, root-relative asset.
			if looksProtocolRelative(assetPath) {
				return ""
			}
			if strings.HasPrefix(assetPath, "/") || strings.HasPrefix(assetPath, "http://") || strings.HasPrefix(assetPath, "https://") {
				return assetPath
			}
			return strings.TrimRight(assetBase, "/") + "/" + strings.TrimLeft(assetPath, "/")
		},
		"link":     NormalizePublicLink,
		"external": ExternalURL,
		"mailto":   Mailto,
		"tel":      Tel,
		"price":    PlanPrice,
		"nonEmpty": NonEmpty,
		"dict":     Dict,
		"add":      func(a, b int) int { return a + b },
		"mul":      func(a, b int) int { return a * b },
		"printf":   fmt.Sprintf,
	}
	for name, fn := range extra {
		if strings.TrimSpace(name) == "" || fn == nil {
			continue
		}
		funcs[name] = fn
	}
	return funcs
}

// NormalizePublicLink rewrites an internal link to its locale-stripped public
// form. Absolute http(s) links are returned unchanged; any other scheme
// (including javascript: and data:), protocol-relative links ("//host/x" and
// backslash variants browsers treat the same), and unparseable links are
// rejected with an empty string. Use Mailto and Tel for mail and phone links.
func NormalizePublicLink(link string) string {
	link = strings.TrimSpace(link)
	if link == "" {
		return ""
	}
	// Reject anything a browser would resolve to an external origin before
	// url.Parse (which does not fold backslashes) can be fooled.
	if looksProtocolRelative(link) {
		return ""
	}

	parsed, err := url.Parse(link)
	if err != nil {
		return ""
	}
	if parsed.Scheme != "" || parsed.Host != "" {
		// Absolute or scheme-qualified: only allow http(s).
		if parsed.Scheme == "http" || parsed.Scheme == "https" {
			return link
		}
		return ""
	}

	pathValue := parsed.Path
	for _, locale := range []string{"en", "pt"} {
		prefix := "/" + locale + "/"
		if after, ok := strings.CutPrefix(pathValue, prefix); ok {
			pathValue = "/" + after
			break
		}
		if pathValue == "/"+locale {
			pathValue = "/"
			break
		}
	}
	if strings.HasPrefix(pathValue, "/home#") || pathValue == "/home" {
		pathValue = "/home"
	}
	parsed.Path = pathValue
	return parsed.String()
}

// ExternalURL validates and normalizes raw as an http(s) URL, returning an
// empty string when it is not a valid external URL.
func ExternalURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if parsed, err := url.Parse(raw); err == nil && parsed.Scheme != "" {
		if parsed.Scheme != "http" && parsed.Scheme != "https" {
			return ""
		}
		if parsed.Host == "" {
			return ""
		}
		return parsed.String()
	}
	parsed, err := url.Parse("https://" + raw)
	if err != nil || parsed.Host == "" {
		return ""
	}
	return parsed.String()
}

// Mailto returns a mailto: URL for a valid email address, or an empty string
// when email is empty or invalid.
func Mailto(email string) string {
	email = strings.TrimSpace(email)
	if email == "" {
		return ""
	}
	if strings.ContainsAny(email, "\r\n<>\"") {
		return ""
	}
	if _, err := mail.ParseAddress(email); err != nil {
		return ""
	}
	return (&url.URL{Scheme: "mailto", Opaque: email}).String()
}

// Tel returns a tel: URL for a valid phone number, or an empty string when
// phone is empty or invalid.
func Tel(phone string) string {
	phone = strings.TrimSpace(phone)
	if phone == "" {
		return ""
	}
	normalized, ok := normalizeTelephone(phone)
	if !ok {
		return ""
	}
	return "tel:" + normalized
}

func normalizeTelephone(phone string) (string, bool) {
	var b strings.Builder
	digits := 0
	for _, r := range phone {
		switch {
		case r >= '0' && r <= '9':
			b.WriteRune(r)
			digits++
		case r == '+' && b.Len() == 0:
			b.WriteRune(r)
		case r == '-' || r == '.' || r == '(' || r == ')':
			b.WriteRune(r)
		case r == ' ' || r == '\t':
			continue
		default:
			return "", false
		}
	}
	if digits == 0 {
		return "", false
	}
	return b.String(), true
}

// PlanPrice formats a plan's MonthlyPrice field as a price string, returning
// "Custom" when no positive price is set.
func PlanPrice(plan any) string {
	price := intField(plan, "MonthlyPrice")
	if price <= 0 {
		return "Custom"
	}
	return fmt.Sprintf("EUR %d", price)
}

func intField(value any, name string) int64 {
	reflected := reflect.ValueOf(value)
	for reflected.Kind() == reflect.Pointer || reflected.Kind() == reflect.Interface {
		if reflected.IsNil() {
			return 0
		}
		reflected = reflected.Elem()
	}
	if reflected.Kind() != reflect.Struct {
		return 0
	}
	field := reflected.FieldByName(name)
	if !field.IsValid() {
		return 0
	}
	switch field.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return field.Int()
	default:
		return 0
	}
}

// NonEmpty returns the first non-blank value (trimmed), or an empty string when
// all values are blank.
func NonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// Dict builds a map from alternating key/value arguments. It returns an error
// if the argument count is odd or any key is not a non-empty string.
func Dict(values ...any) (map[string]any, error) {
	if len(values)%2 != 0 {
		return nil, fmt.Errorf("dict expects key/value pairs")
	}

	out := make(map[string]any, len(values)/2)
	for i := 0; i < len(values); i += 2 {
		key, ok := values[i].(string)
		if !ok || strings.TrimSpace(key) == "" {
			return nil, fmt.Errorf("dict keys must be non-empty strings")
		}
		out[key] = values[i+1]
	}
	return out, nil
}

// PublicAssetURL returns the public URL for a client overlay asset, or an empty
// string when the slug or relative path is empty or unsafe. The slug must be a
// single safe segment matching safeClientSlug (letters, digits, hyphen,
// underscore). The relative path is path-cleaned and rejected if it still
// contains a percent sign or a backslash, so none of literal ("../"),
// percent-encoded ("%2e%2e", "%2f"), or backslash ("..\\") traversal can escape
// PublicAssetBasePath once a browser or server decodes the resulting URL.
// (path.Clean treats only "/" as a separator and browsers fold "\\" to "/", so
// a backslash segment would otherwise survive cleaning.)
func PublicAssetURL(clientSlug, relativePath string) string {
	clientSlug = strings.TrimSpace(clientSlug)
	relativePath, _ = strings.CutPrefix(path.Clean("/"+strings.TrimSpace(relativePath)), "/")
	if !safeClientSlug.MatchString(clientSlug) || relativePath == "." || relativePath == "" ||
		strings.Contains(relativePath, "%") || strings.Contains(relativePath, `\`) {
		return ""
	}
	return path.Join(PublicAssetBasePath, clientSlug, relativePath)
}

// BodyClass builds the overlay homepage <body> class list for the given client
// slug, theme, and experience.
func BodyClass(clientSlug, theme, experience string) string {
	classes := []string{
		"overlay-homepage-body",
		"overlay-homepage-" + ClassToken(clientSlug),
	}
	if strings.TrimSpace(theme) != "" || strings.TrimSpace(experience) != "" {
		classes = append(
			classes,
			"overlay-theme-"+ClassToken(theme),
			"overlay-experience-"+ClassToken(experience),
		)
	}
	return JoinClassNames(classes...)
}

// ClassToken normalizes value into a lowercase, dash-separated CSS class token.
func ClassToken(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var out strings.Builder
	lastDash := false
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			out.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			out.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(out.String(), "-")
}

// JoinClassNames joins non-blank, trimmed class names into a single
// space-separated string.
func JoinClassNames(values ...string) string {
	classes := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			classes = append(classes, trimmed)
		}
	}
	return strings.Join(classes, " ")
}
