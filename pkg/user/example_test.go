// Package user_test — example_test.go carries the runnable pkg.go.dev
// examples for the module's primary entry points: constructing a module with
// the default SQLite store and driving tenant-scoped CRUD through the
// service port.
//
// Validates: REQ-USER-001.
// Per: ADR-0017.
// Discipline: C-14.
package user_test

import (
	"context"
	"fmt"
	"net/http"

	"github.com/septagon-oss/pk-modules/pkg/user"

	_ "modernc.org/sqlite"
)

// ExampleNewModule wires the module against an in-memory SQLite store and
// drives a create/read round trip through the service port. Every by-id read
// is tenant-scoped: Get takes the tenant alongside the id.
func ExampleNewModule() {
	m, err := user.NewModule(user.WithSQLiteDSN("file:example_user?mode=memory&cache=shared"))
	if err != nil {
		fmt.Println("new module:", err)
		return
	}
	ctx := context.Background()

	u := &user.User{TenantID: "tenant_a", Email: "ada@example.test", Username: "ada", Active: true}
	if err := m.Service().Create(ctx, u); err != nil {
		fmt.Println("create:", err)
		return
	}

	got, err := m.Service().Get(ctx, "tenant_a", u.ID)
	if err != nil {
		fmt.Println("get:", err)
		return
	}
	fmt.Println(got.Email)
	// Output: ada@example.test
}

// ExampleModule_HTTPHandler mounts the module's canonical routes on a plain
// net/http mux — the same wiring the starter app performs for all nine
// modules. Compile-only: serving is up to the host.
func ExampleModule_HTTPHandler() {
	m, err := user.NewModule(user.WithSQLiteDSN("file:example_user_http?mode=memory&cache=shared"))
	if err != nil {
		fmt.Println("new module:", err)
		return
	}
	mux := http.NewServeMux()
	m.HTTPHandler().RegisterRoutes(mux)
	// mux now serves GET/POST /api/v1/users and GET/PUT/DELETE /api/v1/users/{id};
	// wrap it with an identity middleware so handlers see the caller's principal.
	_ = mux
}
