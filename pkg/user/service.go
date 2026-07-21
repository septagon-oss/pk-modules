package user

// Implements: REQ-USER-001.
// Per: ADR-0017.
// Discipline: C-14.
// service.go owns the default UserService implementation. Pro can embed
// *service to compose SSO-aware lookups, RLS scoping, and richer RBAC
// without reimplementing CRUD.
//
// ADR: ADR-0009 (ports-only module communication), ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/septagon-oss/pk-core/pkg/security/passhash"

	"github.com/septagon-oss/pk-modules/pkg/tenant"
	"github.com/septagon-oss/pk-modules/pkg/user/store"
)

// ErrUnknownTenant is returned by Create and Update when the user's TenantID
// does not resolve against the wired TenantService. The check only fires
// when a TenantService is wired; in OSS deployments without tenant_management
// the user module still operates standalone.
var ErrUnknownTenant = errors.New("user: unknown tenant_id")

// service is the default UserService implementation. It is intentionally a
// thin layer over store.Store so business rules live in one place.
type service struct {
	store  store.Store
	hasher passhash.Hasher
	tenant tenant.TenantService
}

func newService(s store.Store, h passhash.Hasher, t tenant.TenantService) *service {
	return &service{store: s, hasher: h, tenant: t}
}

// Create inserts a new user. If u.ID is empty, the username is used as the
// ID to keep the OSS surface dependency-free. PassHash should be empty here;
// callers set credentials via SetPassword.
func (s *service) Create(ctx context.Context, u *User) error {
	if err := u.Validate(); err != nil {
		return err
	}
	if err := s.validateTenant(ctx, u.TenantID); err != nil {
		return err
	}
	if strings.TrimSpace(u.ID) == "" {
		u.ID = u.TenantID + ":" + u.Username
	}
	row := toStore(u)
	if err := s.store.Create(ctx, row); err != nil {
		return err
	}
	u.CreatedAt = row.CreatedAt
	u.UpdatedAt = row.UpdatedAt
	return nil
}

// validateTenant ensures the user's tenant_id resolves against the wired
// TenantService when one is configured. When no TenantService is wired
// (standalone OSS deployments), the check is skipped so the user module
// remains usable without tenant_management.
func (s *service) validateTenant(ctx context.Context, tenantID string) error {
	if s.tenant == nil {
		return nil
	}
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		// User.Validate() already rejects empty tenant_id; defensive guard.
		return nil
	}
	_, err := s.tenant.Get(ctx, tenantID)
	if err == nil {
		return nil
	}
	if errors.Is(err, tenant.ErrNotFound) {
		return fmt.Errorf("%w: %q", ErrUnknownTenant, tenantID)
	}
	return fmt.Errorf("user: validate tenant: %w", err)
}

// Get returns the user identified by (tenantID, id). A user in another tenant
// is reported as ErrNotFound.
func (s *service) Get(ctx context.Context, tenantID, id string) (*User, error) {
	if strings.TrimSpace(tenantID) == "" {
		return nil, errors.New("user: tenant_id is required")
	}
	if strings.TrimSpace(id) == "" {
		return nil, errors.New("user: id is required")
	}
	row, err := s.store.Get(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	return fromStore(row), nil
}

// GetByEmail returns a user by tenant-scoped email.
func (s *service) GetByEmail(ctx context.Context, tenantID, email string) (*User, error) {
	if strings.TrimSpace(tenantID) == "" {
		return nil, errors.New("user: tenant_id is required")
	}
	if strings.TrimSpace(email) == "" {
		return nil, errors.New("user: email is required")
	}
	row, err := s.store.GetByEmail(ctx, tenantID, email)
	if err != nil {
		return nil, err
	}
	return fromStore(row), nil
}

// GetByUsername returns a user by tenant-scoped username.
func (s *service) GetByUsername(ctx context.Context, tenantID, username string) (*User, error) {
	if strings.TrimSpace(tenantID) == "" {
		return nil, errors.New("user: tenant_id is required")
	}
	if strings.TrimSpace(username) == "" {
		return nil, errors.New("user: username is required")
	}
	row, err := s.store.GetByUsername(ctx, tenantID, username)
	if err != nil {
		return nil, err
	}
	return fromStore(row), nil
}

// List returns users for a tenant in (limit, offset) pages.
func (s *service) List(ctx context.Context, tenantID string, limit, offset int) ([]*User, error) {
	if strings.TrimSpace(tenantID) == "" {
		return nil, errors.New("user: tenant_id is required")
	}
	rows, err := s.store.List(ctx, tenantID, limit, offset)
	if err != nil {
		return nil, err
	}
	out := make([]*User, 0, len(rows))
	for _, r := range rows {
		out = append(out, fromStore(r))
	}
	return out, nil
}

// Update overwrites a user's mutable fields. Credential changes go through
// SetPassword.
func (s *service) Update(ctx context.Context, u *User) error {
	if err := u.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(u.ID) == "" {
		return errors.New("user: id is required for update")
	}
	if err := s.validateTenant(ctx, u.TenantID); err != nil {
		return err
	}
	row := toStore(u)
	if err := s.store.Update(ctx, row); err != nil {
		return err
	}
	u.UpdatedAt = row.UpdatedAt
	return nil
}

// Delete removes the user identified by (tenantID, id). A user in another
// tenant is reported as ErrNotFound.
func (s *service) Delete(ctx context.Context, tenantID, id string) error {
	if strings.TrimSpace(tenantID) == "" {
		return errors.New("user: tenant_id is required")
	}
	if strings.TrimSpace(id) == "" {
		return errors.New("user: id is required")
	}
	return s.store.Delete(ctx, tenantID, id)
}

// SetPassword hashes plaintext via the configured Hasher and persists the
// resulting encoded representation for the user identified by (tenantID, id).
// A user in another tenant is reported as ErrNotFound.
func (s *service) SetPassword(ctx context.Context, tenantID, id, plaintext string) error {
	if strings.TrimSpace(tenantID) == "" {
		return errors.New("user: tenant_id is required")
	}
	if strings.TrimSpace(id) == "" {
		return errors.New("user: id is required")
	}
	if plaintext == "" {
		return errors.New("user: plaintext is required")
	}
	if s.hasher == nil {
		return errors.New("user: no hasher configured")
	}
	encoded, err := s.hasher.Hash(plaintext)
	if err != nil {
		return err
	}
	return s.store.UpdatePassHash(ctx, tenantID, id, encoded, time.Now().UTC())
}

// VerifyPassword reads the stored hash for (tenantID, id) and validates
// plaintext against it. Returns nil on success; the underlying passhash error
// otherwise. A user in another tenant is reported as ErrNotFound.
func (s *service) VerifyPassword(ctx context.Context, tenantID, id, plaintext string) error {
	if strings.TrimSpace(tenantID) == "" {
		return errors.New("user: tenant_id is required")
	}
	if strings.TrimSpace(id) == "" {
		return errors.New("user: id is required")
	}
	if s.hasher == nil {
		return errors.New("user: no hasher configured")
	}
	row, err := s.store.Get(ctx, tenantID, id)
	if err != nil {
		return err
	}
	if row.PassHash == "" {
		return passhash.ErrMismatch
	}
	return s.hasher.Verify(plaintext, row.PassHash)
}

// toStore converts the public User into the storage representation.
func toStore(u *User) *store.User {
	return &store.User{
		ID:          u.ID,
		TenantID:    u.TenantID,
		Email:       u.Email,
		Username:    u.Username,
		PassHash:    u.PassHash,
		DisplayName: u.DisplayName,
		Active:      u.Active,
		CreatedAt:   u.CreatedAt,
		UpdatedAt:   u.UpdatedAt,
	}
}

// fromStore converts a storage row into the public User.
func fromStore(r *store.User) *User {
	if r == nil {
		return nil
	}
	return &User{
		ID:          r.ID,
		TenantID:    r.TenantID,
		Email:       r.Email,
		Username:    r.Username,
		PassHash:    r.PassHash,
		DisplayName: r.DisplayName,
		Active:      r.Active,
		CreatedAt:   r.CreatedAt,
		UpdatedAt:   r.UpdatedAt,
	}
}
