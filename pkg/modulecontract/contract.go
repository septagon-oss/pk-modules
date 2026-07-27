// Implements: REQ-PORTS-001.
// Per: ADR-0009, ADR-0029.
// Discipline: C-14.

// Package modulecontract makes the module boundary executable.
//
// ADR-0009 says modules communicate through ports, never by reaching into one
// another. Every module here has followed that — but nothing checked it, and a
// rule nothing checks is a rule that decays. A Go package cannot hide part of
// itself from a sibling: pkg/audit exports AuditEmitter (a port other modules
// are meant to use) and NewModule (an implementation detail they are not) from
// the same namespace, so the compiler is indifferent between them.
//
// This package closes that gap the way the rest of the project closes gaps:
// the boundary is declared as data and verified by a test. Ports below is the
// complete list of identifiers a module may reference from a SIBLING module.
// Anything else — constructing another module, touching its service struct,
// importing its store — is a violation with a file and line number.
//
// Two rules are enforced:
//
//   - Depth. A module may import a sibling's root package only. Importing
//     pkg/<other>/store (or any subpackage) means reaching past the port
//     surface into another module's data, which no port can justify.
//   - Surface. Within a sibling's root package, only the identifiers declared
//     in Ports may be referenced.
//
// The declaration is a ratchet: an entry with no remaining user fails too, so
// coupling that goes away stays away instead of leaving a standing permission.
// Widening it is a deliberate, reviewable edit to one table — which is the
// point. Shared infrastructure (portslib, contracttest) is exempt: it is the
// port vocabulary itself, not a peer.
package modulecontract

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// ModulePath is the import prefix under which the business modules live.
const ModulePath = "github.com/septagon-oss/pk-modules/pkg/"

// shared names packages that are not modules but common vocabulary: any module
// may use them freely.
var shared = map[string]bool{
	"portslib":       true,
	"contracttest":   true,
	"modulecontract": true,
	// migrate is schema-evolution machinery every store drives, not a peer
	// module. Its presence here was a decision the guard forced: adopting it
	// across the adapters turned every module into a "consumer" of migrate
	// until this line said otherwise, which is the review step working.
	"migrate": true,
}

// Ports declares, per providing module, the exported identifiers that SIBLING
// modules are permitted to reference. A module absent from this map publishes
// nothing to its peers — the default, and where most modules should stay.
//
// This is the coupling graph of the whole system, in one screen. Adding a line
// is how a new dependency between modules gets reviewed.
var Ports = map[string][]string{
	// The audit emitter is the write side of the audit log. Modules record
	// security-relevant events through it; nothing reads the log this way.
	// Only the interface is a port — Emit is a method reached through it, not a
	// package-level function, which is precisely the distinction the guard
	// enforces and a grep would miss.
	"audit": {"AuditEmitter"},

	// Tenant validation for modules that own tenant-scoped rows, plus the
	// sentinel they compare against when a tenant is absent.
	"tenant": {"TenantService", "ErrNotFound"},

	// The read-only view of a user. Modules resolve an actor; none of them
	// create, mutate, or delete users through this.
	"user": {"User", "UserBoundaryReader"},
}

// Violation is one breach of the boundary, located precisely enough to fix.
type Violation struct {
	Consumer string // the module that reached
	Provider string // the module it reached into
	Ref      string // the identifier or import path involved
	Pos      string // file:line
	Reason   string
}

func (v Violation) String() string {
	return fmt.Sprintf("%s: module %q references %s.%s — %s", v.Pos, v.Consumer, v.Provider, v.Ref, v.Reason)
}

// Verify walks the module tree rooted at dir (the directory holding the module
// packages) and reports every boundary breach. Test files are excluded: a test
// may legitimately construct a neighbouring module to build a fixture, and
// forbidding that would push people toward worse test seams than the boundary
// is worth.
func Verify(dir string) ([]Violation, error) {
	modules, err := moduleNames(dir)
	if err != nil {
		return nil, err
	}

	var violations []Violation
	used := map[string]map[string]bool{} // provider -> identifier -> seen

	for _, consumer := range modules {
		err := filepath.WalkDir(filepath.Join(dir, consumer), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			v, u, err := inspectFile(path, consumer, modules)
			if err != nil {
				return err
			}
			violations = append(violations, v...)
			for provider, idents := range u {
				if used[provider] == nil {
					used[provider] = map[string]bool{}
				}
				for ident := range idents {
					used[provider][ident] = true
				}
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}

	violations = append(violations, staleDeclarations(used)...)
	sort.Slice(violations, func(i, j int) bool { return violations[i].Pos < violations[j].Pos })
	return violations, nil
}

// staleDeclarations reports declared ports that nothing references any more, so
// the table shrinks when coupling is removed instead of keeping a permission
// alive for a caller that no longer exists.
func staleDeclarations(used map[string]map[string]bool) []Violation {
	var out []Violation
	providers := make([]string, 0, len(Ports))
	for provider := range Ports {
		providers = append(providers, provider)
	}
	sort.Strings(providers)
	for _, provider := range providers {
		for _, ident := range Ports[provider] {
			if !used[provider][ident] {
				out = append(out, Violation{
					Provider: provider,
					Ref:      ident,
					Pos:      "modulecontract.Ports",
					Reason: "declared as a port but no module references it any more; " +
						"delete the entry so the declared coupling matches the real coupling",
				})
			}
		}
	}
	return out
}

// inspectFile reports the violations in one file and the sibling identifiers it
// legitimately used.
func inspectFile(path, consumer string, modules []string) ([]Violation, map[string]map[string]bool, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return nil, nil, fmt.Errorf("parse %s: %w", path, err)
	}

	var violations []Violation
	used := map[string]map[string]bool{}
	// local package alias -> provider module
	providerFor := map[string]string{}

	for _, imp := range file.Imports {
		raw, err := strconv.Unquote(imp.Path.Value)
		if err != nil || !strings.HasPrefix(raw, ModulePath) {
			continue
		}
		rest := strings.TrimPrefix(raw, ModulePath)
		top, sub, hasSub := strings.Cut(rest, "/")
		if top == consumer || shared[top] || !slices.Contains(modules, top) {
			continue
		}
		if hasSub {
			violations = append(violations, Violation{
				Consumer: consumer, Provider: top, Ref: rest,
				Pos: fset.Position(imp.Pos()).String(),
				Reason: fmt.Sprintf("imports the %q subpackage of a sibling module; a module's "+
					"internals and data layer are not a port", sub),
			})
			continue
		}
		alias := top
		if imp.Name != nil {
			alias = imp.Name.Name
		}
		providerFor[alias] = top
	}
	if len(providerFor) == 0 {
		return violations, used, nil
	}

	// Only a BARE identifier can name a package. This is what separates a real
	// package reference from a field access that merely reads like one:
	// `s.tenant.Get(...)` parses with a SelectorExpr as its base, not an Ident,
	// so it is correctly ignored, while `tenant.ErrNotFound` is not.
	ast.Inspect(file, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		base, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}
		provider, ok := providerFor[base.Name]
		if !ok {
			return true
		}
		ident := sel.Sel.Name
		if slices.Contains(Ports[provider], ident) {
			if used[provider] == nil {
				used[provider] = map[string]bool{}
			}
			used[provider][ident] = true
			return true
		}
		violations = append(violations, Violation{
			Consumer: consumer, Provider: provider, Ref: ident,
			Pos: fset.Position(sel.Pos()).String(),
			Reason: "not a declared port; either use an existing port, publish a new one " +
				"by adding it to modulecontract.Ports, or invert the dependency",
		})
		return true
	})

	return violations, used, nil
}

// moduleNames lists the module package directories under dir.
func moduleNames(dir string) ([]string, error) {
	entries, err := filepath.Glob(filepath.Join(dir, "*"))
	if err != nil {
		return nil, err
	}
	var out []string
	for _, entry := range entries {
		info, err := filepath.Abs(entry)
		if err != nil {
			return nil, err
		}
		name := filepath.Base(info)
		if shared[name] {
			continue
		}
		matches, _ := filepath.Glob(filepath.Join(entry, "*.go"))
		if len(matches) == 0 {
			continue
		}
		out = append(out, name)
	}
	sort.Strings(out)
	return out, nil
}
