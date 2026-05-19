package user_test

// module_test.go validates the user_management module against its public
// API. Tests live in user_test to ensure the OSS contract is exercised the
// way callers see it.
//
// ADR: ADR-0009 (ports-only module communication), ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

import (
	"context"
	"errors"
	"io/fs"
	"path/filepath"
	"testing"

	pkmodule "github.com/septagon-oss/pk-core/pkg/module"
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
	got, err := m.Service().Get(ctx, u.ID)
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
	got, err := m.Service().Get(ctx, u.ID)
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
	if err := m.Service().Delete(ctx, u.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := m.Service().Get(ctx, u.ID); !errors.Is(err, store.ErrNotFound) {
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
	if err := m.Service().SetPassword(ctx, u.ID, "Sup3rSecret!"); err != nil {
		t.Fatalf("SetPassword: %v", err)
	}
	if err := m.Service().VerifyPassword(ctx, u.ID, "Sup3rSecret!"); err != nil {
		t.Fatalf("VerifyPassword: %v", err)
	}
	err := m.Service().VerifyPassword(ctx, u.ID, "wrong")
	if !errors.Is(err, passhash.ErrMismatch) {
		t.Fatalf("VerifyPassword wrong err = %v, want ErrMismatch", err)
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
	err := m.Service().VerifyPassword(ctx, u.ID, "anything")
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
