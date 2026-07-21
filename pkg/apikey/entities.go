package apikey

// Implements: REQ-APIKEY-001.
// Per: ADR-0017.
// Discipline: C-14.
// entities.go owns the APIKey entity and the descriptor metadata shared by
// handlers, the store layer, and admin pages.
//
// ADR: ADR-0009 (ports-only module communication), ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

import (
	"errors"
	"strings"
	"time"
)

// APIKey is the persisted shape of an API key row. Hash holds the bcrypt
// hash of the full plaintext (prefix + secret); the plaintext is never
// stored after issuance.
type APIKey struct {
	ID         string     `json:"id"`
	TenantID   string     `json:"tenant_id"`
	UserID     string     `json:"user_id"`
	Name       string     `json:"name"`
	Prefix     string     `json:"prefix"`
	Hash       string     `json:"-"`
	Scopes     []string   `json:"scopes"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	RevokedAt  *time.Time `json:"revoked_at,omitempty"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

// EntityName is the stable display name of the APIKey entity.
const EntityName = "APIKey"

// APIPath is the canonical HTTP base path for the API key surface.
const APIPath = "/api/v1/api-keys"

// KeyPrefix is the literal string that appears in front of the random
// section of every issued key. It exists so callers can recognise a
// PlatformKit API key by sight ("pk_…") regardless of database backend.
const KeyPrefix = "pk"

// Validate enforces the small invariants required for storage.
func (k *APIKey) Validate() error {
	if k == nil {
		return errors.New("apikey: nil")
	}
	if strings.TrimSpace(k.ID) == "" {
		return errors.New("apikey: id is required")
	}
	if strings.TrimSpace(k.TenantID) == "" {
		return errors.New("apikey: tenant_id is required")
	}
	if strings.TrimSpace(k.UserID) == "" {
		return errors.New("apikey: user_id is required")
	}
	if strings.TrimSpace(k.Name) == "" {
		return errors.New("apikey: name is required")
	}
	if strings.TrimSpace(k.Hash) == "" {
		return errors.New("apikey: hash is required")
	}
	return nil
}

// IsRevoked reports whether the key has been revoked.
func (k *APIKey) IsRevoked() bool { return k.RevokedAt != nil }

// IsExpired reports whether ExpiresAt is set and in the past relative to now.
func (k *APIKey) IsExpired(now time.Time) bool {
	return k.ExpiresAt != nil && now.After(*k.ExpiresAt)
}
