package tenant_test

// module_test.go validates the tenant_management module against its public
// API. Tests live in tenant_test to ensure the OSS contract is exercised the
// way callers see it.
//
// ADR: ADR-0009 (ports-only module communication), ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

import (
	"context"
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	pkmodule "github.com/septagon-oss/pk-core/pkg/module"
	"github.com/septagon-oss/pk-core/pkg/security/identity"
	"github.com/septagon-oss/pk-modules/pkg/tenant"
	"github.com/septagon-oss/pk-modules/pkg/tenant/store"

	_ "modernc.org/sqlite"
)

// sqliteDSN returns an isolated on-disk sqlite DSN so each test gets a fresh
// schema without depending on shared global memory caches.
func sqliteDSN(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	return "file:" + filepath.Join(dir, "tenants.db") + "?_pragma=journal_mode(WAL)"
}

func newModule(t *testing.T) *tenant.Module {
	t.Helper()
	m, err := tenant.NewModule(tenant.WithSQLiteDSN(sqliteDSN(t)))
	if err != nil {
		t.Fatalf("NewModule: %v", err)
	}
	return m
}

func TestNewModuleDefaults(t *testing.T) {
	t.Parallel()

	m := newModule(t)
	if m.Service() == nil {
		t.Fatalf("Service() is nil")
	}
	if m.HTTPHandler() == nil {
		t.Fatalf("HTTPHandler() is nil")
	}
	if m.Store() == nil {
		t.Fatalf("Store() is nil")
	}
	if m.ContextProvider() == nil {
		t.Fatalf("ContextProvider() is nil — module.go must assign the default contextProvider explicitly")
	}
}

// TestHandlerSelfOnlyBoundary proves the tenant HTTP surface's authorization
// rule: an authenticated caller may act only on its own tenant. It creates two
// tenants, then drives the handler with a principal scoped to tenant "t-self"
// and asserts: reading its own tenant works, reading another tenant's record is
// 404 (not disclosed), an anonymous request is 401, and provisioning a new
// tenant via the API is 403 (a platform operation).
func TestHandlerSelfOnlyBoundary(t *testing.T) {
	t.Parallel()
	m := newModule(t)
	ctx := context.Background()

	for _, id := range []string{"t-self", "t-other"} {
		if err := m.Service().Create(ctx, &tenant.Tenant{ID: id, Slug: id, Name: id}); err != nil {
			t.Fatalf("seed tenant %s: %v", id, err)
		}
	}

	withPrincipal := func(r *http.Request, tenantID string) *http.Request {
		if tenantID == "" {
			return r
		}
		return r.WithContext(identity.ContextWithPrincipal(r.Context(),
			identity.Principal{Subject: "u", TenantID: tenantID, AuthMethod: "session"}))
	}
	do := func(method, path, tenantID string) *httptest.ResponseRecorder {
		req := withPrincipal(httptest.NewRequest(method, path, nil), tenantID)
		rec := httptest.NewRecorder()
		m.HTTPHandler().ServeHTTP(rec, req)
		return rec
	}

	if rec := do(http.MethodGet, "/api/v1/tenants/t-self", "t-self"); rec.Code != http.StatusOK {
		t.Fatalf("GET own tenant = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if rec := do(http.MethodGet, "/api/v1/tenants/t-other", "t-self"); rec.Code != http.StatusNotFound {
		t.Fatalf("GET another tenant = %d, want 404 (cross-tenant read must be denied)", rec.Code)
	}
	if rec := do(http.MethodDelete, "/api/v1/tenants/t-other", "t-self"); rec.Code != http.StatusNotFound {
		t.Fatalf("DELETE another tenant = %d, want 404", rec.Code)
	}
	if rec := do(http.MethodGet, "/api/v1/tenants/t-self", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous GET = %d, want 401", rec.Code)
	}
	if rec := do(http.MethodPost, "/api/v1/tenants", "t-self"); rec.Code != http.StatusForbidden {
		t.Fatalf("POST create tenant = %d, want 403 (platform operation)", rec.Code)
	}
}

func TestHandlerRejectsIDWithSlash(t *testing.T) {
	t.Parallel()
	m := newModule(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/a/b", nil)
	rec := httptest.NewRecorder()
	m.HTTPHandler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", rec.Code, rec.Body.String())
	}
}

func TestNewModuleRequiresStore(t *testing.T) {
	t.Parallel()

	_, err := tenant.NewModule()
	if err == nil {
		t.Fatalf("NewModule() with no store should return an error")
	}
}

func TestServiceCreateThenGet(t *testing.T) {
	t.Parallel()

	m := newModule(t)
	ctx := context.Background()
	tn := &tenant.Tenant{Slug: "acme", Name: "Acme Inc."}
	if err := m.Service().Create(ctx, tn); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if tn.ID == "" {
		t.Fatalf("Create did not assign ID")
	}
	if tn.CreatedAt.IsZero() {
		t.Fatalf("Create did not set CreatedAt")
	}

	got, err := m.Service().Get(ctx, tn.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Slug != "acme" || got.Name != "Acme Inc." {
		t.Fatalf("Get returned unexpected tenant: %+v", got)
	}
}

func TestServiceGetBySlug(t *testing.T) {
	t.Parallel()

	m := newModule(t)
	ctx := context.Background()
	in := &tenant.Tenant{Slug: "globex", Name: "Globex"}
	if err := m.Service().Create(ctx, in); err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := m.Service().GetBySlug(ctx, "globex")
	if err != nil {
		t.Fatalf("GetBySlug: %v", err)
	}
	if got.ID != in.ID {
		t.Fatalf("GetBySlug ID = %q, want %q", got.ID, in.ID)
	}
}

func TestServiceListReturnsAll(t *testing.T) {
	t.Parallel()

	m := newModule(t)
	ctx := context.Background()
	for _, slug := range []string{"a", "b", "c"} {
		if err := m.Service().Create(ctx, &tenant.Tenant{Slug: slug, Name: slug}); err != nil {
			t.Fatalf("Create %q: %v", slug, err)
		}
	}
	list, err := m.Service().List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if got := len(list); got != 3 {
		t.Fatalf("List size = %d, want 3", got)
	}
	if list[0].Slug != "a" || list[2].Slug != "c" {
		t.Fatalf("List not sorted by slug: %+v", list)
	}
}

func TestServiceDeleteRemovesEntity(t *testing.T) {
	t.Parallel()

	m := newModule(t)
	ctx := context.Background()
	tn := &tenant.Tenant{Slug: "evil-corp", Name: "Evil Corp"}
	if err := m.Service().Create(ctx, tn); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := m.Service().Delete(ctx, tn.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := m.Service().Get(ctx, tn.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Get after Delete err = %v, want ErrNotFound", err)
	}
}

func TestServiceUpdateChangesName(t *testing.T) {
	t.Parallel()

	m := newModule(t)
	ctx := context.Background()
	tn := &tenant.Tenant{Slug: "acme", Name: "Acme Inc."}
	if err := m.Service().Create(ctx, tn); err != nil {
		t.Fatalf("Create: %v", err)
	}
	tn.Name = "Acme Corp."
	if err := m.Service().Update(ctx, tn); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got, err := m.Service().Get(ctx, tn.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != "Acme Corp." {
		t.Fatalf("Name after Update = %q", got.Name)
	}
}

func TestDuplicateSlugReturnsError(t *testing.T) {
	t.Parallel()

	m := newModule(t)
	ctx := context.Background()
	a := &tenant.Tenant{ID: "1", Slug: "acme", Name: "Acme"}
	b := &tenant.Tenant{ID: "2", Slug: "acme", Name: "Other Acme"}
	if err := m.Service().Create(ctx, a); err != nil {
		t.Fatalf("first Create: %v", err)
	}
	err := m.Service().Create(ctx, b)
	if !errors.Is(err, store.ErrDuplicateSlug) {
		t.Fatalf("duplicate slug err = %v, want ErrDuplicateSlug", err)
	}
}

func TestContextProviderRoundTrip(t *testing.T) {
	t.Parallel()

	m := newModule(t)
	cp := m.ContextProvider()

	ctx := context.Background()
	if _, ok := cp.CurrentTenantID(ctx); ok {
		t.Fatalf("CurrentTenantID should be absent on a bare context")
	}
	ctx = cp.ContextWithTenant(ctx, "tenant-42")
	got, ok := cp.CurrentTenantID(ctx)
	if !ok || got != "tenant-42" {
		t.Fatalf("CurrentTenantID = (%q, %v), want (%q, true)", got, ok, "tenant-42")
	}
}

func TestComposeReturnsValidComposable(t *testing.T) {
	t.Parallel()

	m := newModule(t)
	c := m.Compose()
	if c == nil {
		t.Fatalf("Compose() returned nil")
	}
	if c.Metadata().ID != tenant.ModuleID {
		t.Fatalf("metadata ID = %q, want %q", c.Metadata().ID, tenant.ModuleID)
	}
	if len(c.Provides()) != 2 {
		t.Fatalf("provides len = %d, want 2 (TenantService + TenantContextProvider)", len(c.Provides()))
	}
	deps := c.Dependencies()
	if len(deps) != 2 {
		t.Fatalf("dependencies len = %d, want 2 (admin + health)", len(deps))
	}
	for _, dep := range deps {
		if dep.Required {
			t.Fatalf("dep %s should be optional", dep.Port.Name)
		}
	}

	// Confirm the catalog validates this Composable end-to-end.
	catalog := pkmodule.NewCatalog().
		Add(pkmodule.NewBundle("tenant-bundle", []pkmodule.Entry{
			{ID: tenant.ModuleID, New: func() pkmodule.Composable { return c }},
		}, []string{tenant.ModuleID})).
		MustBuild()
	if _, err := pkmodule.Compose(catalog); err != nil {
		t.Fatalf("Compose catalog: %v", err)
	}
}

func TestMigrationsEmbedded(t *testing.T) {
	t.Parallel()

	m := newModule(t)
	migFS := m.Migrations()
	entries, err := fs.ReadDir(migFS, ".")
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	found := false
	for _, e := range entries {
		if e.Name() == "0001_initial.up.sql" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("migrations FS missing 0001_initial.up.sql; entries = %+v", entries)
	}

	data, err := fs.ReadFile(migFS, "0001_initial.up.sql")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if len(data) == 0 {
		t.Fatalf("0001_initial.up.sql is empty")
	}
}

func TestEntityValidate(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		in      tenant.Tenant
		wantErr bool
	}{
		{"ok", tenant.Tenant{Slug: "acme", Name: "Acme"}, false},
		{"missing slug", tenant.Tenant{Name: "Acme"}, true},
		{"missing name", tenant.Tenant{Slug: "acme"}, true},
		{"whitespace slug", tenant.Tenant{Slug: "   ", Name: "Acme"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := tc.in.Validate()
			if (err != nil) != tc.wantErr {
				t.Fatalf("Validate err = %v, wantErr=%v", err, tc.wantErr)
			}
		})
	}
}
