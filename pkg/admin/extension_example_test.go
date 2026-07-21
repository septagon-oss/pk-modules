package admin_test

// Validates: REQ-ADMIN-001.
// Per: ADR-0032.
// Discipline: C-14.
// extension_example_test.go demonstrates the Pro-extension pattern: embed
// *admin.Module, add Pro-only fields, override or augment behavior. The
// compile-time check guarantees the OSS Module type stays embeddable.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

import (
	"testing"

	"github.com/septagon-oss/pk-modules/pkg/admin"
)

type proAdminModule struct {
	*admin.Module
	customBrand string
}

func newProAdminModule(t *testing.T, brand string) *proAdminModule {
	t.Helper()
	base, err := admin.NewModule(admin.WithTitle(brand + " Admin"))
	if err != nil {
		t.Fatalf("NewModule: %v", err)
	}
	return &proAdminModule{Module: base, customBrand: brand}
}

func TestProEmbeddingCompiles(t *testing.T) {
	t.Parallel()
	p := newProAdminModule(t, "Acme")
	if p.Title() != "Acme Admin" {
		t.Fatalf("embedded Module.Title() = %q", p.Title())
	}
	if p.customBrand != "Acme" {
		t.Fatal("Pro field lost during embedding")
	}
	// Registrar() still callable through the embedded *Module.
	if p.Registrar() == nil {
		t.Fatal("Registrar() should not be nil")
	}
}
