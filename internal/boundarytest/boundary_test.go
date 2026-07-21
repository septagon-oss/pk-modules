// Validates: REQ-002.
// Per: ADR-0009.
// Discipline: C-14.

// Package boundarytest owns the executable enforcement of the
// no-cross-module-implementation-imports invariant (ADR-0009). Business
// modules communicate only through published contracts/ports; a module must
// never import another module's concrete implementation package (its /store or
// /store/... subpackages). This test walks the module source and fails on any
// violation, so the boundary is guarded mechanically rather than by review.
//
// ADR: ADR-0009 (ports-only module communication), ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package boundarytest

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

const modulePkgPrefix = "github.com/septagon-oss/pk-modules/pkg/"

// storeImport matches another module's concrete store package:
//
//	github.com/septagon-oss/pk-modules/pkg/<module>/store[/...]
var storeImport = regexp.MustCompile(`^github\.com/septagon-oss/pk-modules/pkg/([^/]+)/store(?:/.*)?$`)

// TestNoCrossModuleStoreImports asserts that no package under pkg/<module>/
// imports another module's /store implementation package. Importing your own
// module's store is fine; importing a sibling module's store is the violation
// that reintroduces cross-module coupling.
func TestNoCrossModuleStoreImports(t *testing.T) {
	// The test runs with its own directory as the working dir; the module
	// source root (pkg/) is two levels up from internal/boundarytest.
	pkgRoot, err := filepath.Abs(filepath.Join("..", "..", "pkg"))
	if err != nil {
		t.Fatalf("resolve pkg root: %v", err)
	}
	if _, err := os.Stat(pkgRoot); err != nil {
		t.Fatalf("pkg root not found at %s: %v", pkgRoot, err)
	}

	fset := token.NewFileSet()
	var violations []string

	walkErr := filepath.WalkDir(pkgRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		// Skip tests: composition and test harnesses may legitimately wire
		// concrete stores. The invariant is about production module code.
		if strings.HasSuffix(path, "_test.go") {
			return nil
		}

		// owningModule is the first path segment under pkg/ for this file.
		rel, err := filepath.Rel(pkgRoot, path)
		if err != nil {
			return err
		}
		owningModule := strings.SplitN(filepath.ToSlash(rel), "/", 2)[0]

		f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, imp := range f.Imports {
			ip, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				continue
			}
			m := storeImport.FindStringSubmatch(ip)
			if m == nil {
				continue
			}
			importedModule := m[1]
			if importedModule != owningModule {
				violations = append(violations, rel+" imports "+
					strings.TrimPrefix(ip, modulePkgPrefix)+
					" (another module's concrete store)")
			}
		}
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walk pkg tree: %v", walkErr)
	}

	if len(violations) > 0 {
		t.Fatalf("cross-module store imports violate ADR-0009 (depend on contracts/ports, not another module's /store):\n  %s",
			strings.Join(violations, "\n  "))
	}
}
