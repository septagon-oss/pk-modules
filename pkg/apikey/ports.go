package apikey

// ports.go owns the public interfaces other modules consume. Downstream
// modules should depend on these interfaces — never on *Module or the
// concrete store implementations.
//
// ADR: ADR-0009 (ports-only module communication), ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

import (
	"context"
	"net/http"
	"time"
)

// APIKeyService is the public port for issuing, verifying, listing, and
// revoking API keys.
type APIKeyService interface {
	Issue(ctx context.Context, tenantID, userID, name string, scopes []string, ttl time.Duration) (plaintext string, key *APIKey, err error)
	Verify(ctx context.Context, plaintext string) (*APIKey, error)
	Revoke(ctx context.Context, tenantID, id string) error
	List(ctx context.Context, tenantID string) ([]*APIKey, error)
}

// APIKeyAuthenticator is the HTTP middleware surface other modules consume
// to attach an identity.Principal to the request context when a request
// presents a valid bearer token.
type APIKeyAuthenticator interface {
	Middleware() func(http.Handler) http.Handler
}
