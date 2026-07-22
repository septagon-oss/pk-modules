// Package contracttest provides reusable conformance checks a module author
// runs from a _test.go file to PROVE their custom module upholds PlatformKit's
// invariants — turning "I hope I remembered the tenant predicate" into a
// passing test.
//
// The headline check is AssertTenantScoped: it creates a row in one tenant and
// verifies that a second tenant can neither read nor delete it by ID, and that
// a cross-tenant delete leaves the row intact. A store that forgets its
// `WHERE tenant_id = ?` clause fails this immediately.
//
// Implements: REQ-PORTS-001.
// Per: ADR-0009.
// Discipline: C-14.
package contracttest

import (
	"context"
	"errors"
	"testing"
)

// TenantScopedStore adapts your store to the isolation check via small
// closures, so it works regardless of your concrete signatures. All four are
// required; NotFound is the sentinel your by-ID reads/deletes return for a
// missing-or-other-tenant row (e.g. your own store.ErrNotFound).
type TenantScopedStore struct {
	// Create inserts a fresh row owned by tenantID and returns its ID.
	Create func(ctx context.Context, tenantID string) (id string, err error)
	// Get returns nil if the row identified by (tenantID, id) is visible to
	// that tenant, or NotFound (wrapped is fine) if it is not.
	Get func(ctx context.Context, tenantID, id string) error
	// Delete removes the row identified by (tenantID, id), returning NotFound
	// when nothing matches in that tenant.
	Delete func(ctx context.Context, tenantID, id string) error
	// NotFound is the sentinel Get/Delete return for a cross-tenant or missing
	// row. The check uses errors.Is against it.
	NotFound error
}

// AssertTenantScoped fails t if the store leaks or mutates across tenants. Call
// it from a test:
//
//	func TestNoteStoreIsTenantScoped(t *testing.T) {
//	    s := newTestStore(t)
//	    contracttest.AssertTenantScoped(t, contracttest.TenantScopedStore{
//	        Create:   func(ctx context.Context, tid string) (string, error) { ... },
//	        Get:      func(ctx context.Context, tid, id string) error { _, err := s.Get(ctx, tid, id); return err },
//	        Delete:   s.Delete,
//	        NotFound: store.ErrNotFound,
//	    })
//	}
func AssertTenantScoped(t testing.TB, s TenantScopedStore) {
	t.Helper()
	if s.Create == nil || s.Get == nil || s.Delete == nil || s.NotFound == nil {
		t.Fatal("contracttest: TenantScopedStore requires Create, Get, Delete, and NotFound")
	}
	ctx := context.Background()
	const owner, other = "tenant-owner", "tenant-other"

	id, err := s.Create(ctx, owner)
	if err != nil {
		t.Fatalf("contracttest: Create(owner) failed: %v", err)
	}
	if id == "" {
		t.Fatal("contracttest: Create returned an empty ID")
	}

	// The owner can read its own row.
	if err := s.Get(ctx, owner, id); err != nil {
		t.Fatalf("contracttest: owner cannot read its own row: %v", err)
	}

	// A different tenant must NOT read it — cross-tenant reads fail closed.
	if err := s.Get(ctx, other, id); !errors.Is(err, s.NotFound) {
		t.Fatalf("contracttest: TENANT LEAK — Get(other-tenant, id) = %v, want NotFound. "+
			"Your Get is missing an `AND tenant_id = ?` predicate.", err)
	}

	// A different tenant must NOT delete it — and the row must survive.
	if err := s.Delete(ctx, other, id); !errors.Is(err, s.NotFound) {
		t.Fatalf("contracttest: TENANT MUTATION — Delete(other-tenant, id) = %v, want NotFound. "+
			"Your Delete is missing an `AND tenant_id = ?` predicate.", err)
	}
	if err := s.Get(ctx, owner, id); err != nil {
		t.Fatalf("contracttest: a cross-tenant delete destroyed the owner's row: %v", err)
	}

	// The owner can delete its own row.
	if err := s.Delete(ctx, owner, id); err != nil {
		t.Fatalf("contracttest: owner cannot delete its own row: %v", err)
	}
	if err := s.Get(ctx, owner, id); !errors.Is(err, s.NotFound) {
		t.Fatalf("contracttest: row still readable after the owner deleted it: %v", err)
	}
}
