// Implements: REQ-ADMIN-001.
// Per: ADR-0032.
// Discipline: C-14.

package admin

// shell.go owns the schema-aware reference admin. Modules register typed
// resources and the shell turns those descriptors into accessible lists,
// forms, navigation, and responsive operator pages without a frontend build.

import (
	"bytes"
	"embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"sort"
	"strings"
	"sync"

	"github.com/septagon-oss/pk-core/pkg/security/identity"

	adminstatic "github.com/septagon-oss/pk-modules/pkg/admin/static"
	"github.com/septagon-oss/pk-modules/pkg/portslib"
)

//go:embed templates
var templatesFS embed.FS

// ShellOptions configure a Shell at construction time.
type ShellOptions struct {
	Title    string
	BasePath string
}

// Shell is the in-memory AdminRegistrar implementation. It stores registered
// resources and pages, then renders a single coherent management surface.
type Shell struct {
	title    string
	basePath string

	mu        sync.RWMutex
	resources []portslib.AdminResource
	pages     []portslib.AdminPage
	sidebar   []portslib.SidebarSection

	templates map[string]*template.Template
	static    http.Handler
	css       []byte
}

// NewShell constructs a Shell with the given options. Empty Title and
// BasePath fall back to "PlatformKit Admin" and "/admin" respectively.
func NewShell(opts ShellOptions) *Shell {
	title := strings.TrimSpace(opts.Title)
	if title == "" {
		title = defaultTitle
	}
	basePath := strings.TrimSpace(opts.BasePath)
	if basePath == "" {
		basePath = defaultBasePath
	}
	basePath = "/" + strings.Trim(basePath, "/")

	s := &Shell{title: title, basePath: basePath}
	s.templates = parsePageTemplates()
	s.static = http.StripPrefix(basePath+"/static/", http.FileServer(http.FS(adminstatic.FS())))
	s.css = composeCSS()
	return s
}

func composeCSS() []byte {
	rules, err := fs.ReadFile(adminstatic.FS(), "_admin.css")
	if err != nil {
		rules = nil
	}
	out := make([]byte, 0, len(rules)+256)
	out = append(out, adminTokenCSS()...)
	out = append(out, '\n')
	out = append(out, rules...)
	return out
}

func parsePageTemplates() map[string]*template.Template {
	pages := []string{"home", "entity_list", "entity_form"}
	out := make(map[string]*template.Template, len(pages))
	for _, page := range pages {
		out[page] = template.Must(template.New("admin").Funcs(template.FuncMap{
			"inc": func(value int) int { return value + 1 },
		}).ParseFS(
			templatesFS,
			"templates/layout.tmpl",
			"templates/sidebar.tmpl",
			"templates/"+page+".tmpl",
		))
	}
	return out
}

// Title returns the configured shell title.
func (s *Shell) Title() string { return s.title }

// BasePath returns the configured base path.
func (s *Shell) BasePath() string { return s.basePath }

// RegisterResource adds a typed management surface. Duplicate identifiers or
// incomplete descriptors fail during application construction.
func (s *Shell) RegisterResource(resource portslib.AdminResource) error {
	if err := normalizeResource(&resource); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.resources {
		if existing.ModuleID == resource.ModuleID && existing.EntityName == resource.EntityName {
			return fmt.Errorf("admin: resource %s/%s already registered", resource.ModuleID, resource.EntityName)
		}
	}
	s.resources = append(s.resources, resource)
	return nil
}

func normalizeResource(resource *portslib.AdminResource) error {
	if resource == nil {
		return errors.New("admin: RegisterResource: resource is required")
	}
	resource.ModuleID = strings.TrimSpace(resource.ModuleID)
	resource.EntityName = strings.TrimSpace(resource.EntityName)
	resource.SingularLabel = strings.TrimSpace(resource.SingularLabel)
	resource.PluralLabel = strings.TrimSpace(resource.PluralLabel)
	resource.Description = strings.TrimSpace(resource.Description)
	resource.APIPath = strings.TrimSpace(resource.APIPath)
	resource.IDKey = strings.TrimSpace(resource.IDKey)
	if resource.ModuleID == "" || resource.EntityName == "" || resource.APIPath == "" {
		return errors.New("admin: RegisterResource: moduleID, entityName, and APIPath are required")
	}
	if resource.SingularLabel == "" {
		resource.SingularLabel = humanizeIdentifier(resource.EntityName)
	}
	if resource.PluralLabel == "" {
		resource.PluralLabel = resource.SingularLabel + "s"
	}
	if resource.IDKey == "" {
		resource.IDKey = "id"
	}
	if len(resource.Columns) == 0 {
		return errors.New("admin: RegisterResource: at least one human-readable column is required")
	}

	columns := make(map[string]bool, len(resource.Columns))
	for i := range resource.Columns {
		column := &resource.Columns[i]
		column.Key = strings.TrimSpace(column.Key)
		column.Label = strings.TrimSpace(column.Label)
		if column.Key == "" || column.Label == "" {
			return errors.New("admin: RegisterResource: every column requires a key and label")
		}
		if columns[column.Key] {
			return fmt.Errorf("admin: RegisterResource: duplicate column %q", column.Key)
		}
		columns[column.Key] = true
	}

	fields := make(map[string]bool, len(resource.Fields))
	for i := range resource.Fields {
		field := &resource.Fields[i]
		field.Key = strings.TrimSpace(field.Key)
		field.Label = strings.TrimSpace(field.Label)
		if field.Key == "" || field.Label == "" {
			return errors.New("admin: RegisterResource: every field requires a key and label")
		}
		if fields[field.Key] {
			return fmt.Errorf("admin: RegisterResource: duplicate field %q", field.Key)
		}
		fields[field.Key] = true
		if field.Kind == "" {
			field.Kind = portslib.AdminFieldText
		}
	}
	if err := normalizeRowCondition("edit_when", resource.EditWhen); err != nil {
		return err
	}
	if err := normalizeRowCondition("delete_when", resource.DeleteWhen); err != nil {
		return err
	}
	for i := range resource.Actions {
		action := &resource.Actions[i]
		action.Label = strings.TrimSpace(action.Label)
		action.Method = strings.ToUpper(strings.TrimSpace(action.Method))
		action.PathSuffix = "/" + strings.Trim(action.PathSuffix, "/")
		if action.Label == "" || action.PathSuffix == "/" {
			return errors.New("admin: RegisterResource: every action requires a label and path suffix")
		}
		if action.Method == "" {
			action.Method = http.MethodPost
		}
		switch action.Method {
		case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		default:
			return fmt.Errorf("admin: RegisterResource: unsupported action method %q", action.Method)
		}
		if err := normalizeRowCondition("action visible_when", action.VisibleWhen); err != nil {
			return err
		}
	}
	return nil
}

func normalizeRowCondition(name string, condition *portslib.AdminRowCondition) error {
	if condition == nil {
		return nil
	}
	condition.Field = strings.TrimSpace(condition.Field)
	condition.Value = strings.TrimSpace(condition.Value)
	if condition.Field == "" {
		return fmt.Errorf("admin: RegisterResource: %s requires a field", name)
	}
	switch condition.Operator {
	case portslib.AdminConditionEquals:
		if condition.Value == "" {
			return fmt.Errorf("admin: RegisterResource: %s equals condition requires a value", name)
		}
	case portslib.AdminConditionEmpty, portslib.AdminConditionNotEmpty:
		if condition.Value != "" {
			return fmt.Errorf("admin: RegisterResource: %s %s condition cannot carry a value", name, condition.Operator)
		}
	default:
		return fmt.Errorf("admin: RegisterResource: %s has unsupported operator %q", name, condition.Operator)
	}
	return nil
}

// RegisterPage adds a custom page rendered by the supplied handler.
func (s *Shell) RegisterPage(page portslib.AdminPage) error {
	if strings.TrimSpace(page.ModuleID) == "" {
		return errors.New("admin: RegisterPage: ModuleID required")
	}
	if strings.TrimSpace(page.Path) == "" {
		return errors.New("admin: RegisterPage: Path required")
	}
	if page.Render == nil {
		return errors.New("admin: RegisterPage: Render handler required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.pages {
		if existing.ModuleID == page.ModuleID && existing.Path == page.Path {
			return fmt.Errorf("admin: page %s already registered for module %s", page.Path, page.ModuleID)
		}
	}
	s.pages = append(s.pages, page)
	return nil
}

// RegisterSidebarSection adds a left-nav section.
func (s *Shell) RegisterSidebarSection(section portslib.SidebarSection) error {
	if strings.TrimSpace(section.ModuleID) == "" {
		return errors.New("admin: RegisterSidebarSection: ModuleID required")
	}
	if strings.TrimSpace(section.Label) == "" {
		return errors.New("admin: RegisterSidebarSection: Label required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.sidebar {
		if existing.ModuleID == section.ModuleID && existing.Label == section.Label {
			return fmt.Errorf("admin: sidebar section %q already registered for module %s", section.Label, section.ModuleID)
		}
	}
	s.sidebar = append(s.sidebar, section)
	return nil
}

// Resources returns a snapshot of registered resource descriptors.
func (s *Shell) Resources() []portslib.AdminResource {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]portslib.AdminResource, len(s.resources))
	copy(out, s.resources)
	return out
}

// Pages returns a snapshot of registered custom pages.
func (s *Shell) Pages() []portslib.AdminPage {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]portslib.AdminPage, len(s.pages))
	copy(out, s.pages)
	return out
}

// SidebarSections returns sidebar sections sorted by Order then Label.
func (s *Shell) SidebarSections() []portslib.SidebarSection {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]portslib.SidebarSection, len(s.sidebar))
	copy(out, s.sidebar)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Order != out[j].Order {
			return out[i].Order < out[j].Order
		}
		return out[i].Label < out[j].Label
	})
	return out
}

// ServeHTTP routes admin requests.
func (s *Shell) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set(
		"Content-Security-Policy",
		"default-src 'none'; style-src 'self'; script-src 'self'; connect-src 'self'; img-src 'self' data:; form-action 'self'; base-uri 'none'; frame-ancestors 'none'",
	)
	w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	urlPath := r.URL.Path

	staticPrefix := s.basePath + "/static/"
	if strings.HasPrefix(urlPath, staticPrefix) {
		if urlPath == staticPrefix+"_admin.css" {
			w.Header().Set("Content-Type", "text/css; charset=utf-8")
			w.Header().Set("Cache-Control", "public, max-age=300")
			_, _ = w.Write(s.css)
			return
		}
		s.static.ServeHTTP(w, r)
		return
	}

	if page, ok := s.findPage(urlPath); ok {
		page.Render(w, r)
		return
	}
	if urlPath == s.basePath || urlPath == s.basePath+"/" {
		s.renderHome(w, r)
		return
	}

	if rest, ok := strings.CutPrefix(urlPath, s.basePath+"/"); ok {
		parts := strings.Split(strings.Trim(rest, "/"), "/")
		if len(parts) >= 2 {
			resource, found := s.findResource(parts[0], parts[1])
			if found {
				switch len(parts) {
				case 2:
					s.renderEntityList(w, r, resource)
					return
				case 3:
					switch {
					case parts[2] == "new" && resource.CanCreate:
						s.renderEntityForm(w, r, resource, "")
						return
					case parts[2] != "new" && resource.CanEdit:
						s.renderEntityForm(w, r, resource, parts[2])
						return
					}
				}
			}
		}
	}
	http.NotFound(w, r)
}

func (s *Shell) findPage(urlPath string) (portslib.AdminPage, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, page := range s.pages {
		if page.Path == urlPath {
			return page, true
		}
	}
	return portslib.AdminPage{}, false
}

func (s *Shell) findResource(moduleID, entityName string) (portslib.AdminResource, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, resource := range s.resources {
		if resource.ModuleID == moduleID && resource.EntityName == entityName {
			return resource, true
		}
	}
	return portslib.AdminResource{}, false
}

type shellView struct {
	Title       string
	PageTitle   string
	BasePath    string
	CurrentPath string
	Subject     string
	TenantID    string
	Sidebar     []portslib.SidebarSection
}

func (s *Shell) view(r *http.Request, pageTitle string) shellView {
	principal := identity.PrincipalFromContext(r.Context())
	return shellView{
		Title:       s.title,
		PageTitle:   pageTitle,
		BasePath:    s.basePath,
		CurrentPath: r.URL.Path,
		Subject:     principal.Subject,
		TenantID:    principal.TenantID,
		Sidebar:     s.SidebarSections(),
	}
}

type moduleSummary struct {
	ModuleID    string
	DisplayName string
	Description string
	Resources   []portslib.AdminResource
	Pages       []portslib.AdminPage
}

type homeStats struct {
	Areas       int
	Collections int
	Actions     int
}

type homeData struct {
	shellView
	Modules []moduleSummary
	Stats   homeStats
}

var moduleDisplayNames = map[string]string{
	"tenant_management":       "Tenants",
	"user_management":         "Users",
	"auth_management":         "Authentication",
	"api_key_management":      "API keys",
	"audit_management":        "Audit",
	"content_management":      "Content",
	"notification_management": "Notifications",
	"admin_management":        "Admin",
	"health_management":       "Health",
}

var moduleDescriptions = map[string]string{
	"tenant_management":       "Organization identity, ownership, and tenancy boundaries.",
	"user_management":         "People, access state, and profile records.",
	"auth_management":         "Interactive sessions and sign-in policy.",
	"api_key_management":      "Scoped credentials for automation and integrations.",
	"audit_management":        "Append-only evidence of security and lifecycle events.",
	"content_management":      "Tenant-owned pages, posts, and publishing state.",
	"notification_management": "In-app messages and delivery preferences.",
	"admin_management":        "The operator workspace and its registered surfaces.",
	"health_management":       "Live dependency checks from the composed application.",
}

func humanizeModuleID(id string) string {
	if name, ok := moduleDisplayNames[id]; ok {
		return name
	}
	return humanizeIdentifier(strings.TrimSuffix(id, "_management"))
}

func humanizeIdentifier(value string) string {
	words := strings.FieldsFunc(value, func(r rune) bool {
		return r == '_' || r == '-' || r == '.'
	})
	for i, word := range words {
		if word != "" {
			words[i] = strings.ToUpper(word[:1]) + word[1:]
		}
	}
	if len(words) == 0 {
		return value
	}
	return strings.Join(words, " ")
}

type entityListData struct {
	shellView
	Resource       portslib.AdminResource
	ResourceConfig string
}

type entityFormData struct {
	shellView
	Resource       portslib.AdminResource
	ResourceConfig string
	EntityID       string
}

func (s *Shell) renderHome(w http.ResponseWriter, r *http.Request) {
	modules := s.modulesSummary()
	stats := homeStats{Areas: len(modules)}
	for _, module := range modules {
		stats.Collections += len(module.Resources)
		for _, resource := range module.Resources {
			stats.Actions += boolInt(resource.CanCreate) + boolInt(resource.CanEdit) +
				boolInt(resource.CanDelete) + len(resource.Actions)
		}
	}
	s.render(w, "home", homeData{
		shellView: s.view(r, "Overview"),
		Modules:   modules,
		Stats:     stats,
	})
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func (s *Shell) renderEntityList(
	w http.ResponseWriter,
	r *http.Request,
	resource portslib.AdminResource,
) {
	config, err := encodeResource(resource)
	if err != nil {
		http.Error(w, "admin render: invalid resource descriptor", http.StatusInternalServerError)
		return
	}
	s.render(w, "entity_list", entityListData{
		shellView:      s.view(r, resource.PluralLabel),
		Resource:       resource,
		ResourceConfig: config,
	})
}

func (s *Shell) renderEntityForm(
	w http.ResponseWriter,
	r *http.Request,
	resource portslib.AdminResource,
	id string,
) {
	config, err := encodeResource(resource)
	if err != nil {
		http.Error(w, "admin render: invalid resource descriptor", http.StatusInternalServerError)
		return
	}
	title := "New " + resource.SingularLabel
	if id != "" {
		title = "Edit " + resource.SingularLabel
	}
	s.render(w, "entity_form", entityFormData{
		shellView:      s.view(r, title),
		Resource:       resource,
		ResourceConfig: config,
		EntityID:       id,
	})
}

func encodeResource(resource portslib.AdminResource) (string, error) {
	payload, err := json.Marshal(resource)
	if err != nil {
		return "", err
	}
	return base64.RawStdEncoding.EncodeToString(payload), nil
}

func (s *Shell) render(w http.ResponseWriter, name string, data any) {
	tmpl, ok := s.templates[name]
	if !ok {
		http.Error(w, fmt.Sprintf("admin render: unknown template %q", name), http.StatusInternalServerError)
		return
	}
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, name, data); err != nil {
		http.Error(w, fmt.Sprintf("admin render: %v", err), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(buf.Bytes())
}

func (s *Shell) modulesSummary() []moduleSummary {
	s.mu.RLock()
	defer s.mu.RUnlock()
	byID := map[string]*moduleSummary{}
	order := []string{}
	add := func(id string) *moduleSummary {
		if existing, ok := byID[id]; ok {
			return existing
		}
		summary := &moduleSummary{ModuleID: id}
		byID[id] = summary
		order = append(order, id)
		return summary
	}
	for _, resource := range s.resources {
		summary := add(resource.ModuleID)
		summary.Resources = append(summary.Resources, resource)
	}
	for _, page := range s.pages {
		summary := add(page.ModuleID)
		summary.Pages = append(summary.Pages, page)
	}
	sort.Strings(order)
	out := make([]moduleSummary, 0, len(order))
	for _, id := range order {
		summary := *byID[id]
		summary.DisplayName = humanizeModuleID(id)
		summary.Description = moduleDescriptions[id]
		out = append(out, summary)
	}
	return out
}
