package notification

// entities.go owns the Subscription entity, severity constants, and descriptor
// metadata used by handlers, the store layer, and admin pages. The
// Notification value type is re-used from portslib.Notification so adjacent
// modules can emit notifications without depending on this package.
//
// ADR: ADR-0009 (ports-only module communication), ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

import (
	"errors"
	"strings"
	"time"
)

// Subscription is the entity that records a user's interest in a category
// for a given channel. Subscriptions are tenant-scoped.
type Subscription struct {
	ID        string    `json:"id"`
	TenantID  string    `json:"tenant_id"`
	UserID    string    `json:"user_id"`
	Category  string    `json:"category"`
	Channel   string    `json:"channel"`
	CreatedAt time.Time `json:"created_at"`
}

// Severity values accepted by the OSS notification module. They mirror
// portslib.Notification.Severity semantics.
const (
	SeverityInfo     = "info"
	SeverityWarning  = "warning"
	SeverityCritical = "critical"
)

// ChannelInApp is the default channel name for the built-in in-app channel.
const ChannelInApp = "in_app"

// EntityName is the stable display name of the Notification entity.
const EntityName = "Notification"

// APIPath is the canonical HTTP base path for notification access.
const APIPath = "/api/v1/notifications"

// SubscriptionAPIPath is the canonical HTTP base path for subscription CRUD.
const SubscriptionAPIPath = "/api/v1/notification-subscriptions"

// validateSubscription enforces the small invariants required for storage.
func validateSubscription(s *Subscription) error {
	if s == nil {
		return errors.New("notification: nil subscription")
	}
	if strings.TrimSpace(s.TenantID) == "" {
		return errors.New("notification: tenant_id is required")
	}
	if strings.TrimSpace(s.UserID) == "" {
		return errors.New("notification: user_id is required")
	}
	if strings.TrimSpace(s.Channel) == "" {
		return errors.New("notification: channel is required")
	}
	return nil
}
