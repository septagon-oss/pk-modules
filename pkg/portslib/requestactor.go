// Implements: REQ-005.
// Per: ADR-0009.
// Discipline: C-14.

// requestactor.go owns the single choke point for "the server owns identity".
// Every write handler binds the tenant and attribution fields (author, actor,
// owner) from the authenticated principal returned here — never from the
// request body or query — and every user-owned by-id operation scopes by the
// returned subject. Centralizing it means the rule cannot be re-forgotten one
// field or one module at a time (the class of bug behind the tenant_id, then
// user_id/author_id/actor spoofing findings).
//
// ADR: ADR-0009 (ports-only module communication), ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package portslib

import (
	"net/http"
	"strings"

	"github.com/septagon-oss/pk-core/pkg/security/identity"
)

// RequestActor extracts the authenticated actor — the tenant the caller acts in
// and the subject (their user/principal id) — from the request's identity. Both
// come from the authenticated principal, never from request input, so a handler
// cannot be tricked into acting as another tenant or user via a body/query
// field. It writes 401 and returns ok=false when either is missing (anonymous
// or a principal without a subject).
func RequestActor(w http.ResponseWriter, r *http.Request) (tenantID, subjectID string, ok bool) {
	p := identity.PrincipalFromContext(r.Context())
	tenantID = strings.TrimSpace(p.TenantID)
	subjectID = strings.TrimSpace(p.Subject)
	if tenantID == "" || subjectID == "" {
		http.Error(w, "unauthorized: request identity missing tenant or subject", http.StatusUnauthorized)
		return "", "", false
	}
	return tenantID, subjectID, true
}
