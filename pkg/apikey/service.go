package apikey

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

// service is the default APIKeyService implementation.
type service struct {
	store  store.Store
	hasher passhash.Hasher
	audit  audit.AuditEmitter
	now    func() time.Time
}

func newService(s store.Store, h passhash.Hasher, emitter audit.AuditEmitter) *service {
	return &service{store: s, hasher: h, audit: emitter, now: time.Now}
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

	scopesCopy := append([]string(nil), scopes...)
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
		// verification itself succeeded.
		_ = s.store.UpdateLastUsed(ctx, row.ID, now)
		row.LastUsedAt = &now
		return fromStore(row), nil
	}
	return nil, ErrInvalidKey
}

// Revoke marks a key revoked.
func (s *service) Revoke(ctx context.Context, id string) error {
	if strings.TrimSpace(id) == "" {
		return errors.New("apikey: id is required")
	}
	// Lookup the key first so the audit event can carry the tenant; failure
	// here must not block the actual revoke from running.
	var tenantID string
	if s.audit != nil {
		if row, err := s.store.Get(ctx, id); err == nil && row != nil {
			tenantID = row.TenantID
		}
	}
	if err := s.store.Revoke(ctx, id); err != nil {
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
