package user

// entities.go owns the User entity, descriptor metadata, and validation
// helpers shared by handlers, the store layer, and admin pages.
//
// ADR: ADR-0009 (ports-only module communication), ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

import (
	"errors"
	"fmt"
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

// ErrValidation wraps every field-level validation failure returned by
// Validate. Handlers map errors.Is(err, ErrValidation) to HTTP 400 so a
// malformed request body is never reported as a 500 (a client error is not a
// server fault). It is the module's single "bad input" taxonomy anchor.
var ErrValidation = errors.New("user: validation failed")

// Validate enforces the small invariants required for storage. ID is
// caller-assigned (UUID, ULID, slug — anything stable); the email, username,
// and tenant_id are validated here. Every failure wraps ErrValidation.
func (u *User) Validate() error {
	if u == nil {
		return fmt.Errorf("%w: user is nil", ErrValidation)
	}
	if strings.TrimSpace(u.TenantID) == "" {
		return fmt.Errorf("%w: tenant_id is required", ErrValidation)
	}
	if strings.TrimSpace(u.Email) == "" {
		return fmt.Errorf("%w: email is required", ErrValidation)
	}
	if !strings.Contains(u.Email, "@") {
		return fmt.Errorf("%w: email must contain '@'", ErrValidation)
	}
	if strings.TrimSpace(u.Username) == "" {
		return fmt.Errorf("%w: username is required", ErrValidation)
	}
	return nil
}
