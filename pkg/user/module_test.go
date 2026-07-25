package user_test

// Validates: REQ-USER-001.
// Per: ADR-0017.
// Discipline: C-14.
// module_test.go validates the user_management module against its public
// API. Tests live in user_test to ensure the OSS contract is exercised the
// way callers see it.
//
// ADR: ADR-0009 (ports-only module communication), ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/septagon-oss/pk-modules/pkg/portslib"

	pkmodule "github.com/septagon-oss/pk-core/pkg/module"
	"github.com/septagon-oss/pk-core/pkg/security/identity"
	"github.com/septagon-oss/pk-core/pkg/security/passhash"

	"github.com/septagon-oss/pk-modules/pkg/tenant"
	tenantstore "github.com/septagon-oss/pk-modules/pkg/tenant/store"
	"github.com/septagon-oss/pk-modules/pkg/user"
	"github.com/septagon-oss/pk-modules/pkg/user/store"

	_ "modernc.org/sqlite"
)

// stubTenantService satisfies tenant.TenantService for tenant-validation
// tests. Only Get is exercised; the rest return errors so any accidental
// reliance on them is loud.
type stubTenantService struct {
	known map[string]*tenant.Tenant
}

func newStubTenant(ids ...string) *stubTenantService {
	known := make(map[string]*tenant.Tenant, len(ids))
	for _, id := range ids {
		known[id] = &tenant.Tenant{ID: id, Slug: id, Name: id}
	}
	return &stubTenantService{known: known}
}

func (s *stubTenantService) Get(_ context.Context, id string) (*tenant.Tenant, error) {
	if t, ok := s.known[id]; ok {
		return t, nil
	}
	return nil, tenantstore.ErrNotFound
}

func (*stubTenantService) GetBySlug(context.Context, string) (*tenant.Tenant, error) {
	return nil, errors.New("stub: GetBySlug not implemented")
}

func (*stubTenantService) List(context.Context) ([]*tenant.Tenant, error) {
	return nil, errors.New("stub: List not implemented")
}

func (*stubTenantService) Create(context.Context, *tenant.Tenant) error {
	return errors.New("stub: Create not implemented")
}

func (*stubTenantService) Update(context.Context, *tenant.Tenant) error {
	return errors.New("stub: Update not implemented")
}

func (*stubTenantService) Delete(context.Context, string) error {
	return errors.New("stub: Delete not implemented")
}

func sqliteDSN(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	return "file:" + filepath.Join(dir, "users.db") + "?_pragma=journal_mode(WAL)"
}

func newModule(t *testing.T) *user.Module {
	t.Helper()
	m, err := user.NewModule(user.WithSQLiteDSN(sqliteDSN(t)))
	if err != nil {
		t.Fatalf("NewModule: %v", err)
	}
	return m
}

func makeUser(tenant, name string) *user.User {
	return &user.User{
		TenantID:    tenant,
		Email:       name + "@example.test",
		Username:    name,
		DisplayName: name,
		Active:      true,
	}
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
	if m.Hasher() == nil {
		t.Fatalf("Hasher() is nil")
	}
}

func TestCreateValidatesTenantWhenServiceWired(t *testing.T) {
	t.Parallel()
	stub := newStubTenant("t-1")
	m, err := user.NewModule(
		user.WithSQLiteDSN(sqliteDSN(t)),
		user.WithTenantService(stub),
	)
	if err != nil {
		t.Fatalf("NewModule: %v", err)
	}
	ctx := context.Background()

	// Known tenant — should succeed.
	if err := m.Service().Create(ctx, makeUser("t-1", "alice")); err != nil {
		t.Fatalf("Create with known tenant: %v", err)
	}

	// Unknown tenant — should fail with ErrUnknownTenant.
	err = m.Service().Create(ctx, makeUser("t-ghost", "bob"))
	if !errors.Is(err, user.ErrUnknownTenant) {
		t.Fatalf("Create with unknown tenant err = %v, want ErrUnknownTenant", err)
	}
}

func TestUpdateValidatesTenantWhenServiceWired(t *testing.T) {
	t.Parallel()
	stub := newStubTenant("t-1")
	m, err := user.NewModule(
		user.WithSQLiteDSN(sqliteDSN(t)),
		user.WithTenantService(stub),
	)
	if err != nil {
		t.Fatalf("NewModule: %v", err)
	}
	ctx := context.Background()
	u := makeUser("t-1", "alice")
	if err := m.Service().Create(ctx, u); err != nil {
		t.Fatalf("Create: %v", err)
	}
	// Mutate to an unknown tenant and Update should reject.
	u.TenantID = "t-ghost"
	err = m.Service().Update(ctx, u)
	if !errors.Is(err, user.ErrUnknownTenant) {
		t.Fatalf("Update with unknown tenant err = %v, want ErrUnknownTenant", err)
	}
}

func TestCreateSkipsTenantValidationWhenNoService(t *testing.T) {
	t.Parallel()
	// No WithTenantService — the user module must remain usable standalone.
	m := newModule(t)
	if err := m.Service().Create(context.Background(), makeUser("t-anything", "alice")); err != nil {
		t.Fatalf("Create without tenant service: %v", err)
	}
}

func TestHandlerRejectsIDWithSlash(t *testing.T) {
	t.Parallel()
	m := newModule(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/users/a/b", nil)
	rec := httptest.NewRecorder()
	m.HTTPHandler().ServeHTTP(rec, req)
	// An id is one canonical opaque segment, so "a/b" cannot name an entity at
	// all. That is a malformed request, not a missing one: 404 would imply the
	// identifier was well formed and simply absent.
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

func TestNewModuleRequiresStore(t *testing.T) {
	t.Parallel()
	_, err := user.NewModule()
	if err == nil {
		t.Fatalf("NewModule() with no store should error")
	}
}

func TestServiceCreateThenGet(t *testing.T) {
	t.Parallel()
	m := newModule(t)
	ctx := context.Background()
	u := makeUser("t-1", "alice")
	if err := m.Service().Create(ctx, u); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if u.ID == "" {
		t.Fatalf("Create did not assign ID")
	}
	got, err := m.Service().Get(ctx, "t-1", u.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Email != u.Email || got.Username != u.Username {
		t.Fatalf("Get mismatch: %+v", got)
	}
}

func TestServiceGetByEmail(t *testing.T) {
	t.Parallel()
	m := newModule(t)
	ctx := context.Background()
	u := makeUser("t-1", "bob")
	if err := m.Service().Create(ctx, u); err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := m.Service().GetByEmail(ctx, "t-1", u.Email)
	if err != nil {
		t.Fatalf("GetByEmail: %v", err)
	}
	if got.ID != u.ID {
		t.Fatalf("GetByEmail ID = %q, want %q", got.ID, u.ID)
	}
}

func TestServiceGetByUsername(t *testing.T) {
	t.Parallel()
	m := newModule(t)
	ctx := context.Background()
	u := makeUser("t-1", "carol")
	if err := m.Service().Create(ctx, u); err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := m.Service().GetByUsername(ctx, "t-1", "carol")
	if err != nil {
		t.Fatalf("GetByUsername: %v", err)
	}
	if got.ID != u.ID {
		t.Fatalf("GetByUsername ID = %q, want %q", got.ID, u.ID)
	}
}

func TestServiceList(t *testing.T) {
	t.Parallel()
	m := newModule(t)
	ctx := context.Background()
	for _, name := range []string{"alpha", "bravo", "charlie"} {
		if err := m.Service().Create(ctx, makeUser("t-1", name)); err != nil {
			t.Fatalf("Create %q: %v", name, err)
		}
	}
	// Foreign-tenant user should not appear in t-1 listing.
	if err := m.Service().Create(ctx, makeUser("t-2", "delta")); err != nil {
		t.Fatalf("Create cross-tenant: %v", err)
	}

	list, err := m.Service().List(ctx, "t-1", 10, 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if got := len(list); got != 3 {
		t.Fatalf("List size = %d, want 3", got)
	}
	if list[0].Username != "alpha" || list[2].Username != "charlie" {
		t.Fatalf("List not sorted by username: %+v", list)
	}
}

func TestServiceUpdate(t *testing.T) {
	t.Parallel()
	m := newModule(t)
	ctx := context.Background()
	u := makeUser("t-1", "dave")
	if err := m.Service().Create(ctx, u); err != nil {
		t.Fatalf("Create: %v", err)
	}
	u.DisplayName = "Dave Smith"
	if err := m.Service().Update(ctx, u); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got, err := m.Service().Get(ctx, "t-1", u.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.DisplayName != "Dave Smith" {
		t.Fatalf("display_name = %q, want %q", got.DisplayName, "Dave Smith")
	}
}

func TestServiceDelete(t *testing.T) {
	t.Parallel()
	m := newModule(t)
	ctx := context.Background()
	u := makeUser("t-1", "erin")
	if err := m.Service().Create(ctx, u); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := m.Service().Delete(ctx, "t-1", u.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := m.Service().Get(ctx, "t-1", u.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Get after Delete err = %v, want ErrNotFound", err)
	}
}

func TestSetAndVerifyPassword(t *testing.T) {
	t.Parallel()
	m := newModule(t)
	ctx := context.Background()
	u := makeUser("t-1", "frank")
	if err := m.Service().Create(ctx, u); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := m.Service().SetPassword(ctx, "t-1", u.ID, "Sup3rSecret!"); err != nil {
		t.Fatalf("SetPassword: %v", err)
	}
	if err := m.Service().VerifyPassword(ctx, "t-1", u.ID, "Sup3rSecret!"); err != nil {
		t.Fatalf("VerifyPassword: %v", err)
	}
	err := m.Service().VerifyPassword(ctx, "t-1", u.ID, "wrong")
	if !errors.Is(err, passhash.ErrMismatch) {
		t.Fatalf("VerifyPassword wrong err = %v, want ErrMismatch", err)
	}
}

func TestHandlerCreatesLoginReadyUserForAuthorizedAdmin(t *testing.T) {
	t.Parallel()
	m := newModule(t)
	req := httptest.NewRequest(
		http.MethodPost,
		user.APIPath,
		strings.NewReader(`{
			"email":"new-admin@example.test",
			"username":"new-admin",
			"display_name":"New Admin",
			"password":"correct-horse-battery",
			"active":true
		}`),
	)
	req = req.WithContext(identity.ContextWithPrincipal(req.Context(), identity.Principal{
		Subject:  "operator",
		TenantID: "t-1",
		Scopes:   []string{"admin"},
	}))
	rec := httptest.NewRecorder()

	m.HTTPHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", rec.Code, rec.Body.String())
	}
	created, err := m.Service().GetByUsername(context.Background(), "t-1", "new-admin")
	if err != nil {
		t.Fatalf("GetByUsername: %v", err)
	}
	if err := m.Service().VerifyPassword(
		context.Background(),
		"t-1",
		created.ID,
		"correct-horse-battery",
	); err != nil {
		t.Fatalf("created user's password does not verify: %v", err)
	}
}

func TestHandlerRejectsPasswordWriteWithoutCapability(t *testing.T) {
	t.Parallel()
	m := newModule(t)
	req := httptest.NewRequest(
		http.MethodPost,
		user.APIPath,
		strings.NewReader(`{
			"email":"blocked@example.test",
			"username":"blocked",
			"password":"correct-horse-battery",
			"active":true
		}`),
	)
	req = req.WithContext(identity.ContextWithPrincipal(req.Context(), identity.Principal{
		Subject:  "ordinary-user",
		TenantID: "t-1",
	}))
	rec := httptest.NewRecorder()

	m.HTTPHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("create status = %d, want 403; body = %s", rec.Code, rec.Body.String())
	}
}

func TestHandlerRejectsPasswordResetFromAPIKeyUserWriteScope(t *testing.T) {
	t.Parallel()
	m := newModule(t)
	ctx := context.Background()
	existing := &user.User{
		TenantID:    "t-1",
		Email:       "owner@example.test",
		Username:    "owner",
		DisplayName: "Owner Before",
		Active:      true,
	}
	if err := m.Service().Create(ctx, existing); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := m.Service().SetPassword(ctx, "t-1", existing.ID, "owner-original-password"); err != nil {
		t.Fatalf("SetPassword: %v", err)
	}

	req := httptest.NewRequest(
		http.MethodPut,
		user.APIPath+"/"+encodeID(t, existing.ID),
		strings.NewReader(`{
			"email":"owner@example.test",
			"username":"owner",
			"display_name":"Owner After",
			"password":"attacker-selected-password",
			"active":true
		}`),
	)
	req = req.WithContext(identity.ContextWithPrincipal(req.Context(), identity.Principal{
		Subject:    existing.ID,
		TenantID:   "t-1",
		Scopes:     []string{"users:write"},
		AuthMethod: "api_key",
	}))
	rec := httptest.NewRecorder()

	m.HTTPHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("update status = %d, want 403; body = %s", rec.Code, rec.Body.String())
	}
	if err := m.Service().VerifyPassword(
		ctx,
		"t-1",
		existing.ID,
		"owner-original-password",
	); err != nil {
		t.Fatalf("original password no longer verifies: %v", err)
	}
	if err := m.Service().VerifyPassword(
		ctx,
		"t-1",
		existing.ID,
		"attacker-selected-password",
	); !errors.Is(err, passhash.ErrMismatch) {
		t.Fatalf("attacker-selected password error = %v, want ErrMismatch", err)
	}
	got, err := m.Service().Get(ctx, "t-1", existing.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.DisplayName != "Owner Before" {
		t.Fatalf("DisplayName = %q after rejected password reset, want Owner Before", got.DisplayName)
	}
}

func TestHandlerPreservesActiveWhenUpdateOmitsIt(t *testing.T) {
	t.Parallel()
	m := newModule(t)
	ctx := context.Background()
	existing := &user.User{
		TenantID: "t-1", Email: "active@example.test", Username: "active",
		DisplayName: "Before", Active: true,
	}
	if err := m.Service().Create(ctx, existing); err != nil {
		t.Fatalf("Create: %v", err)
	}

	req := httptest.NewRequest(
		http.MethodPut,
		user.APIPath+"/"+encodeID(t, existing.ID),
		strings.NewReader(`{
			"email":"active@example.test",
			"username":"active",
			"display_name":"After"
		}`),
	)
	req = req.WithContext(identity.ContextWithPrincipal(req.Context(), identity.Principal{
		Subject: "operator", TenantID: "t-1", Scopes: []string{"admin"},
	}))
	rec := httptest.NewRecorder()
	m.HTTPHandler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("update status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	got, err := m.Service().Get(ctx, "t-1", existing.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !got.Active || got.DisplayName != "After" {
		t.Fatalf("updated user = active %v, display %q", got.Active, got.DisplayName)
	}
}

func TestHandlerDefaultsNewUserToActive(t *testing.T) {
	t.Parallel()
	m := newModule(t)
	req := httptest.NewRequest(
		http.MethodPost,
		user.APIPath,
		strings.NewReader(`{"email":"default@example.test","username":"default"}`),
	)
	req = req.WithContext(identity.ContextWithPrincipal(req.Context(), identity.Principal{
		Subject: "operator", TenantID: "t-1", Scopes: []string{"admin"},
	}))
	rec := httptest.NewRecorder()
	m.HTTPHandler().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	var created user.User
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode created user: %v", err)
	}
	if !created.Active {
		t.Fatal("created user defaulted to inactive")
	}
}

func TestUsersWriteKeyCannotModifyOrDeleteItsOwner(t *testing.T) {
	t.Parallel()
	m := newModule(t)
	ctx := context.Background()
	owner := &user.User{
		TenantID: "t-1", Email: "owner-lockout@example.test", Username: "owner-lockout",
		Active: true,
	}
	if err := m.Service().Create(ctx, owner); err != nil {
		t.Fatalf("Create: %v", err)
	}
	principal := identity.Principal{
		Subject: owner.ID, TenantID: "t-1", Scopes: []string{"users:write"}, AuthMethod: "api_key",
	}
	for _, request := range []*http.Request{
		httptest.NewRequest(
			http.MethodPut,
			user.APIPath+"/"+encodeID(t, owner.ID),
			strings.NewReader(`{"email":"owner-lockout@example.test","username":"owner-lockout","active":false}`),
		),
		httptest.NewRequest(http.MethodDelete, user.APIPath+"/"+encodeID(t, owner.ID), nil),
	} {
		request = request.WithContext(identity.ContextWithPrincipal(request.Context(), principal))
		rec := httptest.NewRecorder()
		m.HTTPHandler().ServeHTTP(rec, request)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("%s owner mutation = %d, want 403; body=%s", request.Method, rec.Code, rec.Body.String())
		}
	}
	got, err := m.Service().Get(ctx, "t-1", owner.ID)
	if err != nil {
		t.Fatalf("owner disappeared: %v", err)
	}
	if !got.Active {
		t.Fatal("users:write key deactivated its own credential owner")
	}
}

func TestHandlerRejectsTooLongPasswordBeforeUpdatingProfile(t *testing.T) {
	t.Parallel()
	m := newModule(t)
	ctx := context.Background()
	existing := &user.User{
		TenantID: "t-1", Email: "atomic@example.test", Username: "atomic",
		DisplayName: "Before", Active: true,
	}
	if err := m.Service().Create(ctx, existing); err != nil {
		t.Fatalf("Create: %v", err)
	}
	body := `{
		"email":"atomic@example.test",
		"username":"atomic",
		"display_name":"After",
		"password":"` + strings.Repeat("x", user.MaxPasswordBytes+1) + `",
		"active":true
	}`
	req := httptest.NewRequest(http.MethodPut, user.APIPath+"/"+encodeID(t, existing.ID), strings.NewReader(body))
	req = req.WithContext(identity.ContextWithPrincipal(req.Context(), identity.Principal{
		Subject: "operator", TenantID: "t-1", Scopes: []string{"admin"},
	}))
	rec := httptest.NewRecorder()

	m.HTTPHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("update status = %d, want 400; body = %s", rec.Code, rec.Body.String())
	}
	got, err := m.Service().Get(ctx, "t-1", existing.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.DisplayName != "Before" {
		t.Fatalf("DisplayName = %q after rejected password, want Before", got.DisplayName)
	}
}

func TestVerifyPasswordWithoutSetReturnsMismatch(t *testing.T) {
	t.Parallel()
	m := newModule(t)
	ctx := context.Background()
	u := makeUser("t-1", "grace")
	if err := m.Service().Create(ctx, u); err != nil {
		t.Fatalf("Create: %v", err)
	}
	err := m.Service().VerifyPassword(ctx, "t-1", u.ID, "anything")
	if !errors.Is(err, passhash.ErrMismatch) {
		t.Fatalf("VerifyPassword unset hash err = %v, want ErrMismatch", err)
	}
}

func TestDuplicateEmailReturnsError(t *testing.T) {
	t.Parallel()
	m := newModule(t)
	ctx := context.Background()
	a := &user.User{TenantID: "t-1", Email: "dup@example.test", Username: "first", Active: true}
	b := &user.User{TenantID: "t-1", Email: "dup@example.test", Username: "second", Active: true}
	if err := m.Service().Create(ctx, a); err != nil {
		t.Fatalf("first Create: %v", err)
	}
	err := m.Service().Create(ctx, b)
	if !errors.Is(err, store.ErrDuplicateEmail) {
		t.Fatalf("dup-email err = %v, want ErrDuplicateEmail", err)
	}
}

func TestDuplicateUsernameReturnsError(t *testing.T) {
	t.Parallel()
	m := newModule(t)
	ctx := context.Background()
	a := &user.User{TenantID: "t-1", Email: "a@example.test", Username: "shared", Active: true}
	b := &user.User{TenantID: "t-1", Email: "b@example.test", Username: "shared", Active: true}
	if err := m.Service().Create(ctx, a); err != nil {
		t.Fatalf("first Create: %v", err)
	}
	err := m.Service().Create(ctx, b)
	if !errors.Is(err, store.ErrDuplicateUsername) {
		t.Fatalf("dup-username err = %v, want ErrDuplicateUsername", err)
	}
}

func TestTenantScopedUniquenessAcrossTenants(t *testing.T) {
	t.Parallel()
	m := newModule(t)
	ctx := context.Background()
	a := &user.User{TenantID: "t-1", Email: "shared@example.test", Username: "shared", Active: true}
	b := &user.User{TenantID: "t-2", Email: "shared@example.test", Username: "shared", Active: true}
	if err := m.Service().Create(ctx, a); err != nil {
		t.Fatalf("Create tenant1: %v", err)
	}
	if err := m.Service().Create(ctx, b); err != nil {
		t.Fatalf("Create tenant2 with same email/username should succeed: %v", err)
	}
}

func TestComposeReturnsValidComposable(t *testing.T) {
	t.Parallel()
	m := newModule(t)
	c := m.Compose()
	if c == nil {
		t.Fatalf("Compose() returned nil")
	}
	if c.Metadata().ID != user.ModuleID {
		t.Fatalf("metadata ID = %q, want %q", c.Metadata().ID, user.ModuleID)
	}
	if len(c.Provides()) != 2 {
		t.Fatalf("provides len = %d, want 2", len(c.Provides()))
	}
	deps := c.Dependencies()
	if len(deps) != 3 {
		t.Fatalf("dependencies len = %d, want 3 (admin + health + tenant)", len(deps))
	}
	for _, dep := range deps {
		if dep.Required {
			t.Fatalf("dep %s should be optional", dep.Port.Name)
		}
	}

	catalog := pkmodule.NewCatalog().
		Add(pkmodule.NewBundle("user-bundle", []pkmodule.Entry{
			{ID: user.ModuleID, New: func() pkmodule.Composable { return c }},
		}, []string{user.ModuleID})).
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
		}
	}
	if !found {
		t.Fatalf("migrations FS missing 0001_initial.up.sql; entries = %+v", entries)
	}
}

func TestEntityValidate(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		in      user.User
		wantErr bool
	}{
		{"ok", user.User{TenantID: "t", Email: "a@b.c", Username: "u"}, false},
		{"missing tenant", user.User{Email: "a@b.c", Username: "u"}, true},
		{"missing email", user.User{TenantID: "t", Username: "u"}, true},
		{"bad email", user.User{TenantID: "t", Email: "no-at", Username: "u"}, true},
		{"missing username", user.User{TenantID: "t", Email: "a@b.c"}, true},
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

// encodeID renders an entity id as the canonical opaque path segment the
// handlers require, which is the same form pk-client puts on the wire.
func encodeID(t *testing.T, id string) string {
	t.Helper()
	segment, ok := portslib.EncodeEntityID(id)
	if !ok {
		t.Fatalf("encode entity id %q", id)
	}
	return segment
}

func TestDeleteRejectsSelfDeletion(t *testing.T) {
	t.Parallel()
	m := newModule(t)
	ctx := context.Background()
	u := &user.User{ID: "u-self", TenantID: "t-1", Email: "self@example.test", Username: "self", Active: true}
	if err := m.Service().Create(ctx, u); err != nil {
		t.Fatalf("Create: %v", err)
	}

	req := httptest.NewRequest(http.MethodDelete, user.APIPath+"/id-"+hex.EncodeToString([]byte("u-self")), nil)
	req = req.WithContext(identity.ContextWithPrincipal(ctx, identity.Principal{
		Subject: "u-self", TenantID: "t-1", Scopes: []string{"admin"},
	}))
	rec := httptest.NewRecorder()
	mux := http.NewServeMux()
	m.HTTPHandler().RegisterRoutes(mux)
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("self-delete status = %d, want 409", rec.Code)
	}
	if _, err := m.Service().Get(ctx, "t-1", "u-self"); err != nil {
		t.Fatalf("user was deleted despite the self-delete guard: %v", err)
	}
}
