package audit

// Implements: REQ-AUDIT-001.
// Per: ADR-0017.
// Discipline: C-14.
// service.go owns the default AuditService, AuditReader, and AuditEmitter
// implementations. Pro embeds *service to add retention enforcement,
// partitioning, and forwarding to external SIEMs.
//
// ADR: ADR-0009 (ports-only module communication), ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/septagon-oss/pk-modules/pkg/audit/store"
)

// service is the default AuditService + AuditReader implementation.
type service struct {
	store store.Store
}

func newService(s store.Store) *service { return &service{store: s} }

// Record appends an event to the log. The service assigns EmittedAt if zero,
// defaults severity to info, and generates a sequence-prefixed ID when the
// caller leaves Event.ID blank.
func (s *service) Record(ctx context.Context, e *Event) error {
	if err := e.Validate(); err != nil {
		return err
	}
	if e.Severity == "" {
		e.Severity = SeverityInfo
	}
	if e.EmittedAt.IsZero() {
		e.EmittedAt = time.Now().UTC()
	}
	if strings.TrimSpace(e.ID) == "" {
		id, err := generateID(e.EmittedAt, e.TenantID, e.Action)
		if err != nil {
			return err
		}
		e.ID = id
	}
	row := toStore(e)
	if err := s.store.Append(ctx, row); err != nil {
		return err
	}
	e.EmittedAt = row.EmittedAt
	return nil
}

// Query returns matching events.
func (s *service) Query(ctx context.Context, filter QueryFilter) ([]*Event, error) {
	rows, err := s.store.Query(ctx, store.QueryFilter{
		TenantID: filter.TenantID,
		Actor:    filter.Actor,
		Action:   filter.Action,
		Since:    filter.Since,
		Until:    filter.Until,
		Limit:    filter.Limit,
	})
	if err != nil {
		return nil, err
	}
	out := make([]*Event, 0, len(rows))
	for _, r := range rows {
		out = append(out, fromStore(r))
	}
	return out, nil
}

// emitter is the default AuditEmitter implementation. It carries the
// tenant/actor context for a single emit-call site.
type emitter struct {
	svc      AuditService
	tenantID string
	actor    string
	severity string
}

// Emit serializes details to JSON and records an event.
func (e *emitter) Emit(ctx context.Context, action, resource string, details map[string]any) error {
	if e.svc == nil {
		return errors.New("audit: emitter has no service")
	}
	body := ""
	if len(details) > 0 {
		raw, err := json.Marshal(details)
		if err != nil {
			return fmt.Errorf("audit: marshal details: %w", err)
		}
		body = string(raw)
	}
	return e.svc.Record(ctx, &Event{
		TenantID: e.tenantID,
		Actor:    e.actor,
		Action:   action,
		Resource: resource,
		Severity: e.severity,
		Details:  body,
	})
}

// EmitterFor returns an AuditEmitter pre-bound to the supplied tenant/actor
// pair. Severity defaults to "info" if blank.
func EmitterFor(svc AuditService, tenantID, actor, severity string) AuditEmitter {
	if severity == "" {
		severity = SeverityInfo
	}
	return &emitter{svc: svc, tenantID: tenantID, actor: actor, severity: severity}
}

// generateID synthesizes an event ID without bringing in a UUID dependency.
// The format is `<unix-nano>-<tenant>-<action>-<8 hex chars from crypto/rand>`
// truncated for readability. The random suffix guarantees uniqueness even when
// two events emit in the same nanosecond from the same tenant + action — Pro
// replaces this with ULID.
func generateID(emittedAt time.Time, tenantID, action string) (string, error) {
	var buf [4]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", fmt.Errorf("audit: generateID: %w", err)
	}
	return fmt.Sprintf(
		"%d-%s-%s-%s",
		emittedAt.UTC().UnixNano(),
		shorten(tenantID),
		shorten(action),
		hex.EncodeToString(buf[:]),
	), nil
}

func shorten(s string) string {
	const max = 16
	if len(s) <= max {
		return s
	}
	return s[:max]
}

func toStore(e *Event) *store.Event {
	return &store.Event{
		ID:        e.ID,
		TenantID:  e.TenantID,
		Actor:     e.Actor,
		Action:    e.Action,
		Resource:  e.Resource,
		Severity:  e.Severity,
		Details:   e.Details,
		EmittedAt: e.EmittedAt,
	}
}

func fromStore(r *store.Event) *Event {
	if r == nil {
		return nil
	}
	return &Event{
		ID:        r.ID,
		TenantID:  r.TenantID,
		Actor:     r.Actor,
		Action:    r.Action,
		Resource:  r.Resource,
		Severity:  r.Severity,
		Details:   r.Details,
		EmittedAt: r.EmittedAt,
	}
}
