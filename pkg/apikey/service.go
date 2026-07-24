package apikey

// Implements: REQ-APIKEY-001.
// Per: ADR-0017.
// Discipline: C-14.
// service.go owns the default APIKeyService implementation. Pro embeds
// *service to compose rotation hooks, finer-grained scopes, and per-key
// telemetry without reimplementing issuance and verification.
//
// ADR: ADR-0009 (ports-only module communication), ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/septagon-oss/pk-core/pkg/security/passhash"

	"github.com/septagon-oss/pk-modules/pkg/apikey/store"
	"github.com/septagon-oss/pk-modules/pkg/audit"
)

// Sentinel errors returned by APIKeyService. Verify-side errors are
// collapsed to ErrInvalidKey to avoid leaking whether a prefix exists.
var (
	ErrInvalidKey = errors.New("apikey: invalid key")
	ErrKeyRevoked = errors.New("apikey: key revoked")
	ErrKeyExpired = errors.New("apikey: key expired")
)

var reservedScopes = map[string]struct{}{
	"admin":          {},
	"console:access": {},
}

// defaultAllowedScopes is the machine-capability vocabulary implemented by the
// nine OSS modules and the starter host. Applications add their own module
// scopes with WithAllowedScopes.
var defaultAllowedScopes = []string{
	"api-keys:read",
	"api-keys:write",
	"audit:read",
	"content:read",
	"content:write",
	"metrics:read",
	"notifications:read",
	"notifications:write",
	"tenants:read",
	"tenants:write",
	"users:read",
	"users:write",
}

// service is the default APIKeyService implementation.
type service struct {
	store             store.Store
	hasher            passhash.Hasher
	audit             audit.AuditEmitter
	allowedScopes     map[string]struct{}
	allowedScopeNames []string
	now               func() time.Time
}

func newService(
	s store.Store,
	h passhash.Hasher,
	emitter audit.AuditEmitter,
	additionalScopes []string,
) (*service, error) {
	allowedScopes, allowedScopeNames, err := buildScopeCatalog(additionalScopes)
	if err != nil {
		return nil, err
	}
	return &service{
		store:             s,
		hasher:            h,
		audit:             emitter,
		allowedScopes:     allowedScopes,
		allowedScopeNames: allowedScopeNames,
		now:               time.Now,
	}, nil
}

// Issue generates a new API key and persists its hash. The returned
// plaintext is the only opportunity the caller has to see the full secret —
// this method is the single source of plaintext.
//
// Plaintext layout: "<KeyPrefix>_<base64url(32 random bytes)>".
// The first 12 chars of the random portion are stored as the row Prefix so
// Verify can locate candidate rows without scanning the whole table.
func (s *service) Issue(
	ctx context.Context,
	tenantID, userID, name string,
	scopes []string,
	ttl time.Duration,
) (string, *APIKey, error) {
	tenantID = strings.TrimSpace(tenantID)
	userID = strings.TrimSpace(userID)
	name = strings.TrimSpace(name)
	if tenantID == "" {
		return "", nil, errors.New("apikey: tenant_id is required")
	}
	if userID == "" {
		return "", nil, errors.New("apikey: user_id is required")
	}
	if name == "" {
		return "", nil, errors.New("apikey: name is required")
	}
	scopesCopy, err := s.normalizeScopes(scopes)
	if err != nil {
		return "", nil, err
	}
	secret, err := generateSecret()
	if err != nil {
		return "", nil, fmt.Errorf("apikey: generate secret: %w", err)
	}
	plaintext := KeyPrefix + "_" + secret
	prefix := secret[:12]

	hash, err := s.hasher.Hash(plaintext)
	if err != nil {
		return "", nil, fmt.Errorf("apikey: hash: %w", err)
	}
	id, err := generateID()
	if err != nil {
		return "", nil, fmt.Errorf("apikey: generate id: %w", err)
	}

	encodedScopes, err := json.Marshal(scopesCopy)
	if err != nil {
		return "", nil, fmt.Errorf("apikey: encode scopes: %w", err)
	}

	now := s.now().UTC()
	var expiresAt *time.Time
	if ttl > 0 {
		t := now.Add(ttl)
		expiresAt = &t
	}

	row := &store.APIKey{
		ID:        id,
		TenantID:  tenantID,
		UserID:    userID,
		Name:      name,
		Prefix:    prefix,
		Hash:      hash,
		Scopes:    string(encodedScopes),
		ExpiresAt: expiresAt,
		CreatedAt: now,
	}
	if err := s.store.Create(ctx, row); err != nil {
		return "", nil, err
	}
	out := fromStore(row)
	out.Scopes = scopesCopy
	// Audit emission is best-effort: a transport failure must not roll back the
	// issued key (which is already persisted) or hide it from the caller.
	if s.audit != nil {
		_ = s.audit.Emit(ctx, "apikey.issued", "apikey:"+id, map[string]any{
			"tenant_id": tenantID,
			"user_id":   userID,
			"name":      name,
			"prefix":    prefix,
		})
	}
	return plaintext, out, nil
}

func buildScopeCatalog(additionalScopes []string) (map[string]struct{}, []string, error) {
	names := append([]string(nil), defaultAllowedScopes...)
	names = append(names, additionalScopes...)
	allowed := make(map[string]struct{}, len(names))
	canonical := make([]string, 0, len(names))
	for _, scope := range names {
		scope = strings.TrimSpace(scope)
		if scope == "" {
			return nil, nil, errors.New("apikey: allowed scopes cannot contain an empty value")
		}
		if len(scope) > 100 {
			return nil, nil, errors.New("apikey: each allowed scope must be at most 100 characters")
		}
		if _, reserved := reservedScopes[scope]; reserved {
			return nil, nil, fmt.Errorf("apikey: scope %q is reserved for interactive authorization", scope)
		}
		if _, exists := allowed[scope]; exists {
			continue
		}
		allowed[scope] = struct{}{}
		canonical = append(canonical, scope)
	}
	slices.Sort(canonical)
	return allowed, canonical, nil
}

func (s *service) normalizeScopes(scopes []string) ([]string, error) {
	if len(scopes) > 50 {
		return nil, errors.New("apikey: at most 50 scopes are allowed")
	}
	out := make([]string, 0, len(scopes))
	seen := make(map[string]bool, len(scopes))
	for _, scope := range scopes {
		scope = strings.TrimSpace(scope)
		if scope == "" {
			return nil, errors.New("apikey: scopes cannot contain an empty value")
		}
		if len(scope) > 100 {
			return nil, errors.New("apikey: each scope must be at most 100 characters")
		}
		if _, reserved := reservedScopes[scope]; reserved {
			return nil, fmt.Errorf("apikey: scope %q is reserved for interactive authorization", scope)
		}
		if _, allowed := s.allowedScopes[scope]; !allowed {
			return nil, fmt.Errorf(
				"apikey: unknown scope %q; allowed scopes: %s",
				scope,
				strings.Join(s.allowedScopeNames, ", "),
			)
		}
		if seen[scope] {
			continue
		}
		seen[scope] = true
		out = append(out, scope)
	}
	return out, nil
}

// Verify validates a plaintext API key by locating candidate rows that
// share the same prefix and bcrypt-comparing each candidate's hash. The
// last_used_at column is bumped on success.
func (s *service) Verify(ctx context.Context, plaintext string) (*APIKey, error) {
	plaintext = strings.TrimSpace(plaintext)
	if plaintext == "" {
		return nil, ErrInvalidKey
	}
	prefix, ok := parsePrefix(plaintext)
	if !ok {
		return nil, ErrInvalidKey
	}
	rows, err := s.store.GetByPrefix(ctx, prefix)
	if err != nil {
		return nil, fmt.Errorf("apikey: lookup: %w", err)
	}
	now := s.now().UTC()
	for _, row := range rows {
		if err := s.hasher.Verify(plaintext, row.Hash); err != nil {
			continue
		}
		if row.RevokedAt != nil {
			return nil, ErrKeyRevoked
		}
		if row.ExpiresAt != nil && now.After(*row.ExpiresAt) {
			return nil, ErrKeyExpired
		}
		// Bump last_used_at; failure to update is non-fatal because the
		// verification itself succeeded. The tenant comes from the matched row
		// (GetByPrefix is global; the verified key selects its own tenant).
		_ = s.store.UpdateLastUsed(ctx, row.TenantID, row.ID, now)
		row.LastUsedAt = &now
		return fromStore(row), nil
	}
	return nil, ErrInvalidKey
}

// Revoke marks the key identified by (tenantID, id) revoked. A key in another
// tenant is reported as ErrNotFound.
func (s *service) Revoke(ctx context.Context, tenantID, id string) error {
	if strings.TrimSpace(tenantID) == "" {
		return errors.New("apikey: tenant_id is required")
	}
	if strings.TrimSpace(id) == "" {
		return errors.New("apikey: id is required")
	}
	if err := s.store.Revoke(ctx, tenantID, id); err != nil {
		return err
	}
	if s.audit != nil {
		_ = s.audit.Emit(ctx, "apikey.revoked", "apikey:"+id, map[string]any{
			"id":        id,
			"tenant_id": tenantID,
		})
	}
	return nil
}

// List returns every key for the tenant.
func (s *service) List(ctx context.Context, tenantID string) ([]*APIKey, error) {
	if strings.TrimSpace(tenantID) == "" {
		return nil, errors.New("apikey: tenant_id is required")
	}
	rows, err := s.store.List(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	out := make([]*APIKey, 0, len(rows))
	for _, r := range rows {
		out = append(out, fromStore(r))
	}
	return out, nil
}

// generateSecret returns 32 cryptographically random bytes base64-url
// encoded with no padding. Length is 43 chars.
func generateSecret() (string, error) {
	var buf [32]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf[:]), nil
}

// generateID returns a short URL-safe identifier for the row primary key.
func generateID() (string, error) {
	var buf [12]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf[:]), nil
}

// parsePrefix extracts the indexed prefix (first 12 chars of the random
// portion) from a plaintext API key of the form "<KeyPrefix>_<secret>".
// Returns false when the layout is wrong or the secret is too short.
func parsePrefix(plaintext string) (string, bool) {
	expectedPrefix := KeyPrefix + "_"
	if !strings.HasPrefix(plaintext, expectedPrefix) {
		return "", false
	}
	secret := plaintext[len(expectedPrefix):]
	if len(secret) < 12 {
		return "", false
	}
	return secret[:12], true
}

// fromStore converts a storage row into the public APIKey.
func fromStore(r *store.APIKey) *APIKey {
	if r == nil {
		return nil
	}
	scopes := []string{}
	if r.Scopes != "" {
		_ = json.Unmarshal([]byte(r.Scopes), &scopes)
	}
	return &APIKey{
		ID:         r.ID,
		TenantID:   r.TenantID,
		UserID:     r.UserID,
		Name:       r.Name,
		Prefix:     r.Prefix,
		Hash:       r.Hash,
		Scopes:     scopes,
		LastUsedAt: r.LastUsedAt,
		RevokedAt:  r.RevokedAt,
		ExpiresAt:  r.ExpiresAt,
		CreatedAt:  r.CreatedAt,
	}
}
