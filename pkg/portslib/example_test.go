// Package portslib_test — example_test.go carries the runnable pkg.go.dev
// example for RequestActor, the single choke point through which every write
// handler derives the caller's tenant and subject from the authenticated
// principal (never from the request body).
//
// Validates: REQ-PORTS-001.
// Per: ADR-0009.
// Discipline: C-14.
package portslib_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/septagon-oss/pk-core/pkg/security/identity"

	"github.com/septagon-oss/pk-modules/pkg/portslib"
)

// ExampleRequestActor shows the server-owns-identity pattern: the handler
// binds tenant and attribution from the authenticated principal, so a
// body-supplied tenant_id or user_id can never spoof either. Without a
// principal, RequestActor writes 401 and returns ok=false.
func ExampleRequestActor() {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tenant, subject, ok := portslib.RequestActor(w, r)
		if !ok {
			return // RequestActor already wrote 401
		}
		fmt.Fprintf(w, "acting as %s in %s", subject, tenant)
	})

	// Authenticated request: the identity middleware has put a principal on
	// the context (here injected directly, as tests do).
	req := httptest.NewRequest(http.MethodPost, "/api/v1/notes", nil)
	req = req.WithContext(identity.ContextWithPrincipal(req.Context(),
		identity.Principal{Subject: "user_ada", TenantID: "tenant_a", AuthMethod: "session"}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	fmt.Println(rec.Code, rec.Body.String())

	// Anonymous request: fails closed.
	anon := httptest.NewRequest(http.MethodPost, "/api/v1/notes", nil)
	arec := httptest.NewRecorder()
	h.ServeHTTP(arec, anon)
	fmt.Println(arec.Code)

	// Output:
	// 200 acting as user_ada in tenant_a
	// 401
}
