package notification

// Implements: REQ-NOTIF-002.
// Per: ADR-0017.
// Discipline: C-14.
// convert.go owns the value-shape conversion between portslib.Notification
// (the canonical cross-module value) and store.Notification (the persisted
// row shape). Data is JSON-encoded for storage so the store layer stays
// schema-stable while callers can attach arbitrary structured payloads.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

import (
	"encoding/json"
	"fmt"

	"github.com/septagon-oss/pk-modules/pkg/notification/store"
	"github.com/septagon-oss/pk-modules/pkg/portslib"
)

func toStore(n *portslib.Notification) (*store.Notification, error) {
	if n == nil {
		return nil, nil
	}
	data := ""
	if len(n.Data) > 0 {
		raw, err := json.Marshal(n.Data)
		if err != nil {
			return nil, fmt.Errorf("notification: marshal data: %w", err)
		}
		data = string(raw)
	}
	return &store.Notification{
		ID:        n.ID,
		TenantID:  n.TenantID,
		UserID:    n.UserID,
		Title:     n.Title,
		Body:      n.Body,
		Category:  n.Category,
		Severity:  n.Severity,
		Data:      data,
		EmittedAt: n.EmittedAt,
	}, nil
}

func fromStore(r *store.Notification) *portslib.Notification {
	if r == nil {
		return nil
	}
	var data map[string]any
	if r.Data != "" {
		if err := json.Unmarshal([]byte(r.Data), &data); err != nil {
			data = nil
		}
	}
	return &portslib.Notification{
		ID:        r.ID,
		TenantID:  r.TenantID,
		UserID:    r.UserID,
		Title:     r.Title,
		Body:      r.Body,
		Category:  r.Category,
		Severity:  r.Severity,
		Data:      data,
		EmittedAt: r.EmittedAt,
	}
}

func subToStore(s *Subscription) *store.Subscription {
	return &store.Subscription{
		ID:        s.ID,
		TenantID:  s.TenantID,
		UserID:    s.UserID,
		Category:  s.Category,
		Channel:   s.Channel,
		CreatedAt: s.CreatedAt,
	}
}
