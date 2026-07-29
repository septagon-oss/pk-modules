package branding_test

// Validates: REQ-BRANDING-001.
// Per: ADR-0017.
// Discipline: C-14.
// module_test.go validates the branding_management module against its public
// API. Tests live in branding_test to ensure the OSS contract is exercised
// the way callers see it.
//
// ADR: ADR-0009 (ports-only module communication), ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	pkmodule "github.com/septagon-oss/pk-core/pkg/module"
	corehealth "github.com/septagon-oss/pk-core/pkg/observability/health"

	"github.com/septagon-oss/pk-modules/pkg/branding"
	"github.com/septagon-oss/pk-modules/pkg/branding/store"

	_ "modernc.org/sqlite"
)

// moduleSQLiteDSN returns an isolated on-disk sqlite DSN so each test gets a
// fresh schema without depending on shared global memory caches.
func moduleSQLiteDSN(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	return "file:" + filepath.Join(dir, "branding.db") + "?_pragma=journal_mode(WAL)"
}

func newModule(t *testing.T, opts ...branding.Option) *branding.Module {
	t.Helper()
	allOpts := append([]branding.Option{branding.WithSQLiteDSN(moduleSQLiteDSN(t))}, opts...)
	m, err := branding.NewModule(allOpts...)
	if err != nil {
		t.Fatalf("NewModule: %v", err)
	}
	return m
}

func TestNewModuleWithStoreSucceeds(t *testing.T) {
	t.Parallel()
	m := newModule(t)
	if m.Service() == nil {
		t.Fatalf("Service() is nil")
	}
	if m.Store() == nil {
		t.Fatalf("Store() is nil")
	}
	if m.HTTPHandler() == nil {
		t.Fatalf("HTTPHandler() is nil")
	}
}

func TestNewModuleRequiresStore(t *testing.T) {
	t.Parallel()
	if _, err := branding.NewModule(); err == nil {
		t.Fatalf("NewModule() with no store should return an error")
	}
}

func TestComposeReturnsValidComposable(t *testing.T) {
	t.Parallel()
	m := newModule(t)
	c := m.Compose()
	if c == nil {
		t.Fatalf("Compose() returned nil")
	}
	if c.Metadata().ID != branding.ModuleID {
		t.Fatalf("metadata ID = %q, want %q", c.Metadata().ID, branding.ModuleID)
	}
	if len(c.Provides()) != 1 {
		t.Fatalf("provides len = %d, want 1 (BrandingResolver)", len(c.Provides()))
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
		Add(pkmodule.NewBundle("branding-bundle", []pkmodule.Entry{
			{ID: branding.ModuleID, New: func() pkmodule.Composable { return c }},
		}, []string{branding.ModuleID})).
		MustBuild()
	if _, err := pkmodule.Compose(catalog); err != nil {
		t.Fatalf("Compose catalog: %v", err)
	}
}

// fakeHealthRegistrar is a minimal portslib.HealthRegistrar that captures the
// registered name and checker so the test can invoke it directly.
type fakeHealthRegistrar struct {
	name    string
	checker corehealth.Checker
}

func (f *fakeHealthRegistrar) Register(name string, checker corehealth.Checker) error {
	f.name = name
	f.checker = checker
	return nil
}

// erroringStore is a minimal store.Store whose Get always fails with a fixed
// error, used to drive the health checker's unhealthy branch — a real
// failure (unlike store.ErrNotFound) must fail the check.
type erroringStore struct {
	getErr error
}

func (e *erroringStore) Get(ctx context.Context, tenantID string) (*store.Record, error) {
	return nil, e.getErr
}

func (e *erroringStore) Upsert(ctx context.Context, r *store.Record) error {
	return nil
}

func TestNewModuleRegistersHealthCheck(t *testing.T) {
	t.Parallel()
	reg := &fakeHealthRegistrar{}
	newModule(t, branding.WithHealthRegistrar(reg))

	if reg.name != "branding_management.store" {
		t.Fatalf("registered health check name = %q, want %q", reg.name, "branding_management.store")
	}
	if reg.checker == nil {
		t.Fatalf("registered checker is nil")
	}
	if err := reg.checker.Check(context.Background()); err != nil {
		t.Fatalf("checker.Check on empty store = %v, want nil", err)
	}
}

// TestNewModuleHealthCheckFailsOnStoreError proves registerHealth only
// treats store.ErrNotFound as healthy: a real store error (connection
// failure and the like) must fail the check, not be swallowed alongside it.
func TestNewModuleHealthCheckFailsOnStoreError(t *testing.T) {
	t.Parallel()
	reg := &fakeHealthRegistrar{}
	wantErr := errors.New("branding/sqlite: connection refused")
	m, err := branding.NewModule(
		branding.WithStore(&erroringStore{getErr: wantErr}),
		branding.WithHealthRegistrar(reg),
	)
	if err != nil {
		t.Fatalf("NewModule: %v", err)
	}
	if m.Store() == nil {
		t.Fatalf("Store() is nil")
	}
	if reg.checker == nil {
		t.Fatalf("registered checker is nil")
	}
	if err := reg.checker.Check(context.Background()); !errors.Is(err, wantErr) {
		t.Fatalf("checker.Check = %v, want %v", err, wantErr)
	}
}

func TestNewModuleWithoutHealthRegistrarSkipsRegistration(t *testing.T) {
	t.Parallel()
	// No WithHealthRegistrar: NewModule must not fail when the dependency is
	// simply absent (it's optional).
	m := newModule(t)
	if m == nil {
		t.Fatalf("NewModule returned nil module")
	}
}
