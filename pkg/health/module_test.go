package health_test

// module_test.go validates the health_management module against its public
// API. Tests live in health_test to ensure the OSS contract is exercised the
// way callers see it.
//
// ADR: ADR-0009 (ports-only module communication), ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	pkmodule "github.com/septagon-oss/pk-core/pkg/module"
	corehealth "github.com/septagon-oss/pk-core/pkg/observability/health"

	"github.com/septagon-oss/pk-modules/pkg/health"
)

func TestNewModuleDefaults(t *testing.T) {
	t.Parallel()
	m := health.NewModule()
	if m.Service() == nil {
		t.Fatalf("Service() is nil")
	}
	if m.Registrar() == nil {
		t.Fatalf("Registrar() is nil")
	}
	if m.HTTPHandler() == nil {
		t.Fatalf("HTTPHandler() is nil")
	}
	if m.Registry() == nil {
		t.Fatalf("Registry() is nil")
	}
}

func TestRegisterCheck(t *testing.T) {
	t.Parallel()
	m := health.NewModule()
	ok := corehealth.CheckerFunc(func(context.Context) error { return nil })
	if err := m.Registrar().Register("storage", ok); err != nil {
		t.Fatalf("Register: %v", err)
	}
	res := m.Service().Check(context.Background())
	if res.Status != health.StatusHealthy {
		t.Fatalf("Status = %q, want healthy", res.Status)
	}
	if len(res.Components) != 1 {
		t.Fatalf("len(components) = %d, want 1", len(res.Components))
	}
	if res.Components[0].Name != "storage" {
		t.Fatalf("component name = %q", res.Components[0].Name)
	}
}

func TestRegisterRejectsEmptyName(t *testing.T) {
	t.Parallel()
	m := health.NewModule()
	ok := corehealth.CheckerFunc(func(context.Context) error { return nil })
	if err := m.Registrar().Register("", ok); err == nil {
		t.Fatalf("Register with empty name should error")
	}
}

func TestRegisterRejectsNilChecker(t *testing.T) {
	t.Parallel()
	m := health.NewModule()
	if err := m.Registrar().Register("storage", nil); err == nil {
		t.Fatalf("Register with nil checker should error")
	}
}

func TestCheckAggregatesHealthy(t *testing.T) {
	t.Parallel()
	m := health.NewModule()
	for _, name := range []string{"db", "cache", "queue"} {
		_ = m.Registrar().Register(name, corehealth.CheckerFunc(func(context.Context) error { return nil }))
	}
	res := m.Service().Check(context.Background())
	if res.Status != health.StatusHealthy {
		t.Fatalf("Status = %q, want healthy", res.Status)
	}
	if len(res.Components) != 3 {
		t.Fatalf("len(components) = %d, want 3", len(res.Components))
	}
}

func TestCheckAggregatesUnhealthyOnFailure(t *testing.T) {
	t.Parallel()
	m := health.NewModule()
	_ = m.Registrar().Register("ok", corehealth.CheckerFunc(func(context.Context) error { return nil }))
	_ = m.Registrar().Register("bad", corehealth.CheckerFunc(func(context.Context) error { return errors.New("disk full") }))
	res := m.Service().Check(context.Background())
	if res.Status != health.StatusUnhealthy {
		t.Fatalf("Status = %q, want unhealthy", res.Status)
	}
}

func TestHTTPHandlerReturns200WhenHealthy(t *testing.T) {
	t.Parallel()
	m := health.NewModule()
	_ = m.Registrar().Register("ok", corehealth.CheckerFunc(func(context.Context) error { return nil }))

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	m.HTTPHandler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body health.Result
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Status != health.StatusHealthy {
		t.Fatalf("body status = %q, want healthy", body.Status)
	}
}

func TestHTTPHandlerReturns503WhenUnhealthy(t *testing.T) {
	t.Parallel()
	m := health.NewModule()
	_ = m.Registrar().Register("bad", corehealth.CheckerFunc(func(context.Context) error { return errors.New("broken") }))

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	m.HTTPHandler().ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

func TestComposeReturnsValidComposable(t *testing.T) {
	t.Parallel()
	m := health.NewModule()
	c := m.Compose()
	if c == nil {
		t.Fatalf("Compose() returned nil")
	}
	if c.Metadata().ID != health.ModuleID {
		t.Fatalf("metadata ID = %q, want %q", c.Metadata().ID, health.ModuleID)
	}
	if len(c.Provides()) != 2 {
		t.Fatalf("provides len = %d, want 2", len(c.Provides()))
	}
	deps := c.Dependencies()
	if len(deps) != 1 {
		t.Fatalf("dependencies len = %d, want 1", len(deps))
	}
	for _, dep := range deps {
		if dep.Required {
			t.Fatalf("dep %s should be optional", dep.Port.Name)
		}
	}

	catalog := pkmodule.NewCatalog().
		Add(pkmodule.NewBundle("health-bundle", []pkmodule.Entry{
			{ID: health.ModuleID, New: func() pkmodule.Composable { return c }},
		}, []string{health.ModuleID})).
		MustBuild()
	if _, err := pkmodule.Compose(catalog); err != nil {
		t.Fatalf("Compose catalog: %v", err)
	}
}
