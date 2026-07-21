package notification

// service.go owns the default NotificationService implementation. Create
// iterates every registered channel in order; the first error short-circuits
// the fan-out. The default in-app channel persists notifications to the
// store, so GetByUser/MarkRead read back what was written.
//
// Pro embeds *service to add digesting, deduplication, throttling, and
// async dispatch without changing the OSS surface.
//
// ADR: ADR-0009 (ports-only module communication), ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/septagon-oss/pk-modules/pkg/audit"
	"github.com/septagon-oss/pk-modules/pkg/notification/store"
	"github.com/septagon-oss/pk-modules/pkg/portslib"
)

// service is the default NotificationService implementation.
type service struct {
	store    store.Store
	channels []portslib.NotificationChannel
	audit    audit.AuditEmitter
}

func newService(s store.Store, channels []portslib.NotificationChannel, em audit.AuditEmitter) *service {
	return &service{store: s, channels: channels, audit: em}
}

// Create dispatches a notification through every registered channel in order.
// The OSS contract is "first error wins": dispatch short-circuits when any
// channel returns an error.
func (s *service) Create(ctx context.Context, n *portslib.Notification) error {
	if n == nil {
		return errors.New("notification: nil notification")
	}
	if strings.TrimSpace(n.TenantID) == "" {
		return errors.New("notification: tenant_id is required")
	}
	if strings.TrimSpace(n.UserID) == "" {
		return errors.New("notification: user_id is required")
	}
	if n.Severity == "" {
		n.Severity = SeverityInfo
	}
	switch n.Severity {
	case SeverityInfo, SeverityWarning, SeverityCritical:
	default:
		return errors.New("notification: severity must be info, warning, or critical")
	}
	if n.EmittedAt.IsZero() {
		n.EmittedAt = time.Now().UTC()
	}
	if strings.TrimSpace(n.ID) == "" {
		n.ID = generateID(n.EmittedAt, n.TenantID, n.UserID)
	}
	for _, ch := range s.channels {
		if ch == nil {
			continue
		}
		if err := ch.Deliver(ctx, *n); err != nil {
			return fmt.Errorf("notification: channel %q deliver: %w", ch.Name(), err)
		}
	}
	s.emit(ctx, n, "notification.dispatched")
	return nil
}

// GetByUser returns notifications for a (tenant, user) ordered newest-first.
func (s *service) GetByUser(ctx context.Context, tenantID, userID string, limit, offset int) ([]*portslib.Notification, error) {
	rows, err := s.store.GetByUser(ctx, tenantID, userID, limit, offset)
	if err != nil {
		return nil, err
	}
	out := make([]*portslib.Notification, 0, len(rows))
	for _, r := range rows {
		out = append(out, fromStore(r))
	}
	return out, nil
}

// MarkRead sets read_at to now for the notification identified by
// (tenantID, id). A notification in another tenant is reported as ErrNotFound.
func (s *service) MarkRead(ctx context.Context, tenantID, id string) error {
	return s.store.MarkRead(ctx, tenantID, id, time.Now().UTC())
}

// Subscribe persists a subscription row.
func (s *service) Subscribe(ctx context.Context, sub *Subscription) error {
	if err := validateSubscription(sub); err != nil {
		return err
	}
	if sub.CreatedAt.IsZero() {
		sub.CreatedAt = time.Now().UTC()
	}
	if strings.TrimSpace(sub.ID) == "" {
		sub.ID = generateID(sub.CreatedAt, sub.TenantID, sub.UserID+":"+sub.Channel)
	}
	return s.store.AddSubscription(ctx, subToStore(sub))
}

// Unsubscribe removes the subscription identified by (tenantID, id). A
// subscription in another tenant is reported as ErrNotFound.
func (s *service) Unsubscribe(ctx context.Context, tenantID, subscriptionID string) error {
	return s.store.RemoveSubscription(ctx, tenantID, subscriptionID)
}

// emit fans a notification dispatch event to the optional audit emitter.
func (s *service) emit(ctx context.Context, n *portslib.Notification, action string) {
	if s.audit == nil || n == nil {
		return
	}
	_ = s.audit.Emit(ctx, action, "notification:"+n.ID, map[string]any{
		"user_id":  n.UserID,
		"severity": n.Severity,
		"category": n.Category,
	})
}

// generateID synthesizes a deterministic-ish notification ID without bringing
// in a UUID dependency.
func generateID(emittedAt time.Time, tenantID, key string) string {
	return fmt.Sprintf("%d-%s-%s", emittedAt.UTC().UnixNano(), shorten(tenantID), shorten(key))
}

func shorten(s string) string {
	const max = 32
	if len(s) <= max {
		return s
	}
	return s[:max]
}

// inAppChannel is the built-in NotificationChannel that persists every
// delivery to the store. It implements portslib.NotificationChannel.
type inAppChannel struct {
	store store.Store
}

// Name returns the canonical channel name.
func (c *inAppChannel) Name() string { return ChannelInApp }

// Deliver persists a notification by converting the portslib value to a row.
func (c *inAppChannel) Deliver(ctx context.Context, n portslib.Notification) error {
	row, err := toStore(&n)
	if err != nil {
		return err
	}
	return c.store.Create(ctx, row)
}
