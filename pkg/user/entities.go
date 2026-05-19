package user

// entities.go owns the User entity, descriptor metadata, and validation
// helpers shared by handlers, the store layer, and admin pages.
//
// ADR: ADR-0009 (ports-only module communication), ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

import (
	"errors"
	"strings"
	"time"
)

// User is the entity managed by the user_management module.
type User struct {
	ID          string    `json:"id"`
	TenantID    string    `json:"tenant_id"`
	Email       string    `json:"email"`
	Username    string    `json:"username"`
	PassHash    string    `json:"-"`
	DisplayName string    `json:"display_name"`
	Active      bool      `json:"active"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// EntityName is the stable display name of the User entity.
const EntityName = "User"

// APIPath is the canonical HTTP base path for user CRUD.
const APIPath = "/api/v1/users"

// Validate enforces the small invariants required for storage. ID is
// caller-assigned (UUID, ULID, slug — anything stable); the email, username,
// and tenant_id are validated here.
func (u *User) Validate() error {
	if u == nil {
		return errors.New("user: nil")
	}
	if strings.TrimSpace(u.TenantID) == "" {
		return errors.New("user: tenant_id is required")
	}
	if strings.TrimSpace(u.Email) == "" {
		return errors.New("user: email is required")
	}
	if !strings.Contains(u.Email, "@") {
		return errors.New("user: email must contain '@'")
	}
	if strings.TrimSpace(u.Username) == "" {
		return errors.New("user: username is required")
	}
	return nil
}
