// Package overlay renders client-owned homepage overlay templates.
package overlay

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
	"strings"
)

const PublicAssetBasePath = "/assets/overlays"

type RenderInput struct {
	TemplateSource string
	View           any
	AssetBase      string
	Funcs          template.FuncMap
}

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

func FuncMap(assetBase string, extra template.FuncMap) template.FuncMap {
	funcs := template.FuncMap{
		"asset": func(assetPath string) string {
			assetPath = strings.TrimSpace(assetPath)
			if assetPath == "" {
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
	}
	for name, fn := range extra {
		if strings.TrimSpace(name) == "" || fn == nil {
			continue
		}
		funcs[name] = fn
	}
	return funcs
}

func NormalizePublicLink(link string) string {
	link = strings.TrimSpace(link)
	if link == "" {
		return ""
	}

	parsed, err := url.Parse(link)
	if err != nil {
		return link
	}
	if parsed.Host != "" || parsed.Scheme != "" {
		return link
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

func NonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

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

func PublicAssetURL(clientSlug, relativePath string) string {
	clientSlug = strings.Trim(strings.TrimSpace(clientSlug), "/")
	relativePath, _ = strings.CutPrefix(path.Clean("/"+strings.TrimSpace(relativePath)), "/")
	if clientSlug == "" || relativePath == "." || relativePath == "" {
		return ""
	}
	return path.Join(PublicAssetBasePath, clientSlug, relativePath)
}

func BodyClass(clientSlug, theme, experience string) string {
	classes := []string{
		"overlay-homepage-body",
		"overlay-homepage-" + ClassToken(clientSlug),
	}
	if strings.TrimSpace(theme) != "" || strings.TrimSpace(experience) != "" {
		classes = append(classes,
			"overlay-theme-"+ClassToken(theme),
			"overlay-experience-"+ClassToken(experience),
		)
	}
	return JoinClassNames(classes...)
}

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

func JoinClassNames(values ...string) string {
	classes := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			classes = append(classes, trimmed)
		}
	}
	return strings.Join(classes, " ")
}
