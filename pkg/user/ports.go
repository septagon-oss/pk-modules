package user

// ports.go owns the public interfaces other modules consume. Downstream
// modules should depend on these interfaces — never on *Module or the
// concrete store implementations.
//
// ADR: ADR-0009 (ports-only module communication), ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

import "context"

// UserService is the Pro-consumable port. Other modules depend on this
// (not on the concrete Module type) for user CRUD operations.
type UserService interface {
	Create(ctx context.Context, u *User) error
	Get(ctx context.Context, id string) (*User, error)
	GetByEmail(ctx context.Context, tenantID, email string) (*User, error)
	GetByUsername(ctx context.Context, tenantID, username string) (*User, error)
	List(ctx context.Context, tenantID string, limit, offset int) ([]*User, error)
	Update(ctx context.Context, u *User) error
	Delete(ctx context.Context, id string) error
	SetPassword(ctx context.Context, id, plaintext string) error
	VerifyPassword(ctx context.Context, id, plaintext string) error
}

// UserBoundaryReader is the Pro-consumable read-only port used by adjacent
// modules (audit, api_key, notification, auth) that need to resolve user
// identity without holding a full UserService reference.
type UserBoundaryReader interface {
	Get(ctx context.Context, id string) (*User, error)
	GetByEmail(ctx context.Context, tenantID, email string) (*User, error)
	GetByUsername(ctx context.Context, tenantID, username string) (*User, error)
}

// UserBoundaryRoleManager is a placeholder role-management port. Roles are
// minimal in v0.0.0; full RBAC ships with pk-pro.
type UserBoundaryRoleManager interface {
	AssignRole(ctx context.Context, userID, role string) error
	Roles(ctx context.Context, userID string) ([]string, error)
}
