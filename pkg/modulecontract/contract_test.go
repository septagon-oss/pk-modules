// Validates: REQ-PORTS-001.
// Per: ADR-0009.
// Discipline: C-14.

package modulecontract_test

// contract_test.go runs the boundary guard against this repository's own
// modules. It is the difference between "modules talk through ports" being a
// convention and being a fact.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/septagon-oss/pk-modules/pkg/modulecontract"
)

// moduleRoot is the directory holding the module packages (this package's
// parent).
func moduleRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("working directory: %v", err)
	}
	return filepath.Dir(wd)
}

func TestModulesRespectTheDeclaredPortBoundary(t *testing.T) {
	t.Parallel()

	violations, err := modulecontract.Verify(moduleRoot(t))
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	for _, v := range violations {
		t.Errorf("%s", v)
	}
	if len(violations) > 0 {
		t.Logf("\n%d boundary violation(s). Modules communicate through declared ports "+
			"(ADR-0009); the permitted surface is modulecontract.Ports.", len(violations))
	}
}

// TestGuardCatchesAViolation proves the guard actually fails on a breach —
// without this, a guard that silently matched nothing would look identical to a
// clean codebase. It compiles a fake module tree that reaches past the port
// surface in both ways the guard is meant to catch.
func TestGuardCatchesAViolation(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	write := func(rel, src string) {
		t.Helper()
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// A provider module with a legitimate port and an implementation detail.
	write("audit/audit.go", `package audit
type AuditEmitter interface{ Emit() }
type Module struct{}
func NewModule() *Module { return nil }
`)
	write("audit/store/store.go", `package store
type Store struct{}
`)

	// A consumer that (1) constructs the sibling module, (2) imports its store,
	// and (3) uses a genuine port, which must NOT be reported.
	write("content/content.go", `package content

import (
	"github.com/septagon-oss/pk-modules/pkg/audit"
	auditstore "github.com/septagon-oss/pk-modules/pkg/audit/store"
)

type Service struct {
	emitter audit.AuditEmitter
	store   *auditstore.Store
}

func Build() *audit.Module { return audit.NewModule() }
`)

	violations, err := modulecontract.Verify(root)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}

	joined := make([]string, 0, len(violations))
	for _, v := range violations {
		joined = append(joined, v.String())
	}
	all := strings.Join(joined, "\n")

	// Reaching into the sibling's data layer.
	if !strings.Contains(all, "audit/store") {
		t.Errorf("guard missed the subpackage import.\ngot:\n%s", all)
	}
	// Constructing the sibling module.
	if !strings.Contains(all, "NewModule") {
		t.Errorf("guard missed the cross-module constructor call.\ngot:\n%s", all)
	}
	// The declared port must not be reported.
	for _, v := range violations {
		if v.Ref == "AuditEmitter" {
			t.Errorf("guard flagged the declared port %q as a violation", v.Ref)
		}
	}
}

// TestStaleDeclarationsAreReported pins the ratchet: a port nobody uses is
// itself a finding, so removing coupling also removes the permission.
func TestStaleDeclarationsAreReported(t *testing.T) {
	t.Parallel()

	// An empty tree references nothing, so every declared port is stale.
	violations, err := modulecontract.Verify(t.TempDir())
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	declared := 0
	for _, list := range modulecontract.Ports {
		declared += len(list)
	}
	if len(violations) != declared {
		t.Fatalf("stale-port report = %d violations, want %d (one per declared port)",
			len(violations), declared)
	}
	for _, v := range violations {
		if !strings.Contains(v.Reason, "no module references it") {
			t.Errorf("unexpected violation in an empty tree: %s", v)
		}
	}
}
