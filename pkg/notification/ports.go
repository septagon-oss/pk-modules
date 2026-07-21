package notification

// Implements: REQ-NOTIF-002.
// Per: ADR-0017.
// Discipline: C-14.
// ports.go owns the public interfaces other modules consume. Downstream
// modules should depend on these interfaces — never on *Module or the
// concrete store implementations.
//
// ADR: ADR-0009 (ports-only module communication), ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

import (
	"context"

	"github.com/septagon-oss/pk-modules/pkg/portslib"
)

// NotificationService is the Pro-consumable port. Other modules call Create
// to persist and dispatch notifications; the OSS implementation iterates
// every registered channel in order.
type NotificationService interface {
	Create(ctx context.Context, n *portslib.Notification) error
	GetByUser(ctx context.Context, tenantID, userID string, limit, offset int) ([]*portslib.Notification, error)
	MarkRead(ctx context.Context, tenantID, userID, notificationID string) error
	Subscribe(ctx context.Context, sub *Subscription) error
	Unsubscribe(ctx context.Context, tenantID, userID, subscriptionID string) error
}

// NotificationSubscriber lets other modules register additional channels at
// runtime (Pro uses this to add the mail/SMS/push adapters without rewiring
// the module).
type NotificationSubscriber interface {
	Subscribe(ctx context.Context, eventType string, handler portslib.NotificationChannel) error
}
