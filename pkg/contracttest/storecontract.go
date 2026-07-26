// storecontract.go — conformance checks for the parts of the store contract
// that AssertTenantScoped does not reach: the list surface, tenant
// reassignment through Update, and lifecycle rows that must stop being listed.
//
// Each check encodes a defect class observed in real adapters, so a store that
// reintroduces one fails a test instead of shipping. Like AssertTenantScoped,
// every check adapts to your concrete signatures through closures, so it works
// for any backend — SQLite, Postgres, or a service-backed adapter.
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

// ListScopedStore adapts your store's list surface to the isolation check.
// AssertTenantScoped proves by-ID reads are scoped; this proves the list is
// too, which is a separate query and a separate chance to omit the predicate.
type ListScopedStore struct {
	// Create inserts a fresh row owned by tenantID and returns its ID.
	Create func(ctx context.Context, tenantID string) (id string, err error)
	// List returns the IDs visible to tenantID.
	List func(ctx context.Context, tenantID string) (ids []string, err error)
}

// AssertListTenantScoped fails t if one tenant's list returns another tenant's
// rows. Call it alongside AssertTenantScoped:
//
//	contracttest.AssertListTenantScoped(t, contracttest.ListScopedStore{
//	    Create: func(ctx context.Context, tid string) (string, error) { ... },
//	    List:   func(ctx context.Context, tid string) ([]string, error) { ... },
//	})
func AssertListTenantScoped(t testing.TB, s ListScopedStore) {
	t.Helper()
	if s.Create == nil || s.List == nil {
		t.Fatal("contracttest: ListScopedStore requires Create and List")
	}
	ctx := context.Background()
	const owner, other = "tenant-owner", "tenant-other"

	ownerID, err := s.Create(ctx, owner)
	if err != nil {
		t.Fatalf("contracttest: Create(owner) failed: %v", err)
	}
	otherID, err := s.Create(ctx, other)
	if err != nil {
		t.Fatalf("contracttest: Create(other) failed: %v", err)
	}

	ownerRows, err := s.List(ctx, owner)
	if err != nil {
		t.Fatalf("contracttest: List(owner) failed: %v", err)
	}
	if !contains(ownerRows, ownerID) {
		t.Fatalf("contracttest: List(owner) omitted the owner's own row %q", ownerID)
	}
	if contains(ownerRows, otherID) {
		t.Fatalf("contracttest: TENANT LEAK — List(owner) returned another tenant's row %q. "+
			"Your List is missing a `WHERE tenant_id = ?` predicate.", otherID)
	}
}

// TenantImmutableStore adapts your store's update surface to the reassignment
// check. The store contract treats a row's tenant as fixed at creation: an
// update may change a row's data, never which tenant owns it.
type TenantImmutableStore struct {
	// Create inserts a fresh row owned by tenantID and returns its ID.
	Create func(ctx context.Context, tenantID string) (id string, err error)
	// UpdateReassigning applies an update to the row identified by
	// (tenantID, id) that claims the row now belongs to newTenantID. Build the
	// value your Update takes with its tenant field set to newTenantID and
	// return whatever your store returns — an error is a perfectly good way to
	// refuse, and so is ignoring the field.
	UpdateReassigning func(ctx context.Context, tenantID, id, newTenantID string) error
	// Get returns nil if the row identified by (tenantID, id) is visible to
	// that tenant, or NotFound if it is not.
	Get func(ctx context.Context, tenantID, id string) error
	// NotFound is the sentinel Get returns for a cross-tenant or missing row.
	NotFound error
}

// AssertUpdateCannotReassignTenant fails t if Update can move a row into
// another tenant. A store may refuse with an error or silently ignore the
// field; both pass. What must not happen is the row changing hands.
func AssertUpdateCannotReassignTenant(t testing.TB, s TenantImmutableStore) {
	t.Helper()
	if s.Create == nil || s.UpdateReassigning == nil || s.Get == nil || s.NotFound == nil {
		t.Fatal("contracttest: TenantImmutableStore requires Create, UpdateReassigning, Get, and NotFound")
	}
	ctx := context.Background()
	const owner, other = "tenant-owner", "tenant-other"

	id, err := s.Create(ctx, owner)
	if err != nil {
		t.Fatalf("contracttest: Create(owner) failed: %v", err)
	}

	// Refusing outright is a valid implementation, so the error is not asserted.
	_ = s.UpdateReassigning(ctx, owner, id, other)

	if err := s.Get(ctx, owner, id); err != nil {
		t.Fatalf("contracttest: TENANT REASSIGNED — the owner lost row %q after an update "+
			"claiming tenant %q: %v. A row's tenant is fixed at creation; ignore the "+
			"field on update or reject the write.", id, other, err)
	}
	if err := s.Get(ctx, other, id); !errors.Is(err, s.NotFound) {
		t.Fatalf("contracttest: TENANT REASSIGNED — tenant %q can now read row %q = %v, want NotFound. "+
			"Update wrote the caller-supplied tenant instead of preserving the stored one.", other, id, err)
	}
}

// LifecycleStore adapts a store whose rows can be retired without being
// removed — revoked API keys, deactivated users, archived records. The
// contract is that the retiring operation is the delete an operator sees, so a
// retired row must stop appearing in the default list.
type LifecycleStore struct {
	// Create inserts a fresh, live row owned by tenantID and returns its ID.
	Create func(ctx context.Context, tenantID string) (id string, err error)
	// Retire performs the soft delete — revoke, deactivate, archive — on the
	// row identified by (tenantID, id) without removing it.
	Retire func(ctx context.Context, tenantID, id string) error
	// List returns the IDs the store lists for tenantID by default.
	List func(ctx context.Context, tenantID string) (ids []string, err error)
}

// AssertRetiredHiddenFromList fails t if a retired row keeps appearing in the
// default list. Operators read a re-appearing row as a delete that silently
// failed, and callers that filter only on an explicit status flag reintroduce
// this every time.
func AssertRetiredHiddenFromList(t testing.TB, s LifecycleStore) {
	t.Helper()
	if s.Create == nil || s.Retire == nil || s.List == nil {
		t.Fatal("contracttest: LifecycleStore requires Create, Retire, and List")
	}
	ctx := context.Background()
	const owner = "tenant-owner"

	id, err := s.Create(ctx, owner)
	if err != nil {
		t.Fatalf("contracttest: Create(owner) failed: %v", err)
	}
	rows, err := s.List(ctx, owner)
	if err != nil {
		t.Fatalf("contracttest: List before retire failed: %v", err)
	}
	if !contains(rows, id) {
		t.Fatalf("contracttest: List omitted the live row %q before it was retired", id)
	}

	if err := s.Retire(ctx, owner, id); err != nil {
		t.Fatalf("contracttest: Retire(owner, %q) failed: %v", id, err)
	}

	rows, err = s.List(ctx, owner)
	if err != nil {
		t.Fatalf("contracttest: List after retire failed: %v", err)
	}
	if contains(rows, id) {
		t.Fatalf("contracttest: RETIRED ROW STILL LISTED — %q appears in List after being retired. "+
			"Exclude retired rows in the store predicate; a caller that has to pass a status "+
			"filter to get this right will forget.", id)
	}

	// Retiring twice is a no-op, not an error: the row is already in the state
	// the caller asked for.
	if err := s.Retire(ctx, owner, id); err != nil {
		t.Fatalf("contracttest: retiring an already-retired row returned %v, want nil "+
			"(the operation is idempotent)", err)
	}
}

func contains(ids []string, want string) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}
