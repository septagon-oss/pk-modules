package admin

// shell.go owns the in-memory AdminRegistrar implementation and the HTTP
// renderer for the admin shell. The Shell stores entity-CRUD descriptors,
// custom pages, and sidebar sections registered by sibling modules at boot
// time, then serves them through a small template-driven set of routes.
//
// Routes:
//
//	GET <basePath>                              → home dashboard
//	GET <basePath>/static/<file>                → embedded static asset
//	GET <basePath>/<moduleID>/<entity>          → entity list (calls APIPath via fetch)
//	GET <basePath>/<moduleID>/<entity>/new      → create form
//	GET <basePath>/<moduleID>/<entity>/<id>     → detail/edit form
//	GET <page.Path>                             → custom page (absolute path)
//
// Custom pages register with an absolute Path (eg "/admin/tenants"); the Shell
// matches them by full URL path so they coexist with the generated entity
// routes.
//
// ADR: ADR-0009 (ports-only module communication), ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

import (
	"embed"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"path"
	"sort"
	"strings"
	"sync"

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

// EntityCRUD is the descriptor stored when a module calls RegisterEntityCRUD.
type EntityCRUD struct {
	ModuleID   string
	EntityName string
	APIPath    string
}

// Shell is the in-memory AdminRegistrar implementation. It stores registered
// pages and renders them. Exported so Pro can introspect or extend behavior
// while still satisfying portslib.AdminRegistrar.
type Shell struct {
	title    string
	basePath string

	mu       sync.RWMutex
	entities []EntityCRUD
	pages    []portslib.AdminPage
	sidebar  []portslib.SidebarSection

	templates *template.Template
	static    http.Handler
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

	s := &Shell{
		title:    title,
		basePath: basePath,
	}
	s.templates = template.Must(template.ParseFS(templatesFS,
		"templates/layout.tmpl",
		"templates/sidebar.tmpl",
		"templates/home.tmpl",
		"templates/entity_list.tmpl",
		"templates/entity_form.tmpl",
	))
	s.static = http.StripPrefix(basePath+"/static/", http.FileServer(http.FS(adminstatic.FS())))
	return s
}

// Title returns the configured shell title.
func (s *Shell) Title() string { return s.title }

// BasePath returns the configured base path.
func (s *Shell) BasePath() string { return s.basePath }

// RegisterEntityCRUD adds a CRUD page for the given entity. Duplicate
// (moduleID, entityName) pairs return an error.
func (s *Shell) RegisterEntityCRUD(moduleID, entityName, apiPath string) error {
	moduleID = strings.TrimSpace(moduleID)
	entityName = strings.TrimSpace(entityName)
	apiPath = strings.TrimSpace(apiPath)
	if moduleID == "" || entityName == "" || apiPath == "" {
		return errors.New("admin: RegisterEntityCRUD: moduleID, entityName, apiPath required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range s.entities {
		if e.ModuleID == moduleID && e.EntityName == entityName {
			return fmt.Errorf("admin: entity %s/%s already registered", moduleID, entityName)
		}
	}
	s.entities = append(s.entities, EntityCRUD{
		ModuleID:   moduleID,
		EntityName: entityName,
		APIPath:    apiPath,
	})
	return nil
}

// RegisterPage adds a custom page rendered by the supplied handler. Duplicate
// (moduleID, path) pairs return an error.
func (s *Shell) RegisterPage(p portslib.AdminPage) error {
	if strings.TrimSpace(p.ModuleID) == "" {
		return errors.New("admin: RegisterPage: ModuleID required")
	}
	if strings.TrimSpace(p.Path) == "" {
		return errors.New("admin: RegisterPage: Path required")
	}
	if p.Render == nil {
		return errors.New("admin: RegisterPage: Render handler required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.pages {
		if existing.ModuleID == p.ModuleID && existing.Path == p.Path {
			return fmt.Errorf("admin: page %s already registered for module %s", p.Path, p.ModuleID)
		}
	}
	s.pages = append(s.pages, p)
	return nil
}

// RegisterSidebarSection adds a left-nav section.
func (s *Shell) RegisterSidebarSection(sec portslib.SidebarSection) error {
	if strings.TrimSpace(sec.ModuleID) == "" {
		return errors.New("admin: RegisterSidebarSection: ModuleID required")
	}
	if strings.TrimSpace(sec.Label) == "" {
		return errors.New("admin: RegisterSidebarSection: Label required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.sidebar {
		if existing.ModuleID == sec.ModuleID && existing.Label == sec.Label {
			return fmt.Errorf("admin: sidebar section %q already registered for module %s", sec.Label, sec.ModuleID)
		}
	}
	s.sidebar = append(s.sidebar, sec)
	return nil
}

// Entities returns a snapshot of registered entity-CRUD descriptors.
func (s *Shell) Entities() []EntityCRUD {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]EntityCRUD, len(s.entities))
	copy(out, s.entities)
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

// SidebarSections returns sidebar sections sorted by Order then Label. Stable
// secondary order keeps test assertions deterministic.
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

// ServeHTTP routes admin requests. See package docs for the route table.
func (s *Shell) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	urlPath := r.URL.Path

	// Static assets.
	staticPrefix := s.basePath + "/static/"
	if strings.HasPrefix(urlPath, staticPrefix) {
		s.static.ServeHTTP(w, r)
		return
	}

	// Custom pages take precedence on exact-path match so module-supplied
	// routes like "/admin/tenants" resolve before the generic entity router.
	if page, ok := s.findPage(urlPath); ok {
		page.Render(w, r)
		return
	}

	// Home.
	if urlPath == s.basePath || urlPath == s.basePath+"/" {
		s.renderHome(w, r)
		return
	}

	// Entity routes: <basePath>/<moduleID>/<entityName>(/(new|<id>))?
	if rest, ok := strings.CutPrefix(urlPath, s.basePath+"/"); ok {
		parts := strings.Split(strings.Trim(rest, "/"), "/")
		if len(parts) >= 2 {
			moduleID, entityName := parts[0], parts[1]
			entity, found := s.findEntity(moduleID, entityName)
			if found {
				switch len(parts) {
				case 2:
					s.renderEntityList(w, r, entity)
					return
				case 3:
					if parts[2] == "new" {
						s.renderEntityForm(w, r, entity, "")
					} else {
						s.renderEntityForm(w, r, entity, parts[2])
					}
					return
				}
			}
		}
	}

	http.NotFound(w, r)
}

func (s *Shell) findPage(urlPath string) (portslib.AdminPage, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, p := range s.pages {
		if p.Path == urlPath {
			return p, true
		}
	}
	return portslib.AdminPage{}, false
}

func (s *Shell) findEntity(moduleID, entityName string) (EntityCRUD, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, e := range s.entities {
		if e.ModuleID == moduleID && e.EntityName == entityName {
			return e, true
		}
	}
	return EntityCRUD{}, false
}

// moduleSummary groups entities and pages for the home template.
type moduleSummary struct {
	ModuleID string
	Entities []EntityCRUD
	Pages    []portslib.AdminPage
}

type homeData struct {
	Title     string
	PageTitle string
	BasePath  string
	Sidebar   []portslib.SidebarSection
	Modules   []moduleSummary
}

type entityListData struct {
	Title     string
	PageTitle string
	BasePath  string
	Sidebar   []portslib.SidebarSection
	Entity    EntityCRUD
}

type entityFormData struct {
	Title     string
	PageTitle string
	BasePath  string
	Sidebar   []portslib.SidebarSection
	Entity    EntityCRUD
	EntityID  string
}

func (s *Shell) renderHome(w http.ResponseWriter, _ *http.Request) {
	data := homeData{
		Title:     s.title,
		PageTitle: "Dashboard",
		BasePath:  s.basePath,
		Sidebar:   s.SidebarSections(),
		Modules:   s.modulesSummary(),
	}
	s.render(w, "home", data)
}

func (s *Shell) renderEntityList(w http.ResponseWriter, _ *http.Request, e EntityCRUD) {
	data := entityListData{
		Title:     s.title,
		PageTitle: e.EntityName,
		BasePath:  s.basePath,
		Sidebar:   s.SidebarSections(),
		Entity:    e,
	}
	s.render(w, "entity_list", data)
}

func (s *Shell) renderEntityForm(w http.ResponseWriter, _ *http.Request, e EntityCRUD, id string) {
	title := "New " + e.EntityName
	if id != "" {
		title = e.EntityName + " " + id
	}
	data := entityFormData{
		Title:     s.title,
		PageTitle: title,
		BasePath:  s.basePath,
		Sidebar:   s.SidebarSections(),
		Entity:    e,
		EntityID:  id,
	}
	s.render(w, "entity_form", data)
}

func (s *Shell) render(w http.ResponseWriter, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.templates.ExecuteTemplate(w, name, data); err != nil {
		http.Error(w, fmt.Sprintf("admin render: %v", err), http.StatusInternalServerError)
	}
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
		ms := &moduleSummary{ModuleID: id}
		byID[id] = ms
		order = append(order, id)
		return ms
	}
	for _, e := range s.entities {
		add(e.ModuleID).Entities = append(add(e.ModuleID).Entities, e)
	}
	for _, p := range s.pages {
		add(p.ModuleID).Pages = append(add(p.ModuleID).Pages, p)
	}
	sort.Strings(order)
	out := make([]moduleSummary, 0, len(order))
	for _, id := range order {
		out = append(out, *byID[id])
	}
	return out
}

// joinBase is a small helper for tests that need to construct URLs against
// the shell's base path without re-implementing the trim-and-join rules.
func (s *Shell) joinBase(elem ...string) string {
	return path.Join(append([]string{s.basePath}, elem...)...)
}
