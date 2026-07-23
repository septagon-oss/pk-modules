// Implements: REQ-AUTH-001.
// Per: ADR-0028.
// Discipline: C-14.

package auth

import "github.com/septagon-oss/pk-modules/pkg/portslib"

// Authentication deliberately registers no generic collection: the public
// API exposes only owner-scoped lookup and logout by bearer-secret session ID,
// so pretending a list endpoint exists would create a broken admin page.
func registerAdmin(portslib.AdminRegistrar) error {
	return nil
}
