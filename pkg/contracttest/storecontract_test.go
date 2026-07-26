// Validates: REQ-PORTS-001.
// Per: ADR-0009.
// Discipline: C-14.
package contracttest_test

import (
	"context"
	"errors"
	"testing"

	"github.com/septagon-oss/pk-modules/pkg/contracttest"
	userstore "github.com/septagon-oss/pk-modules/pkg/user/store"
)

// memStore is a minimal correct store: every read is tenant-predicated, the
// tenant is fixed at creation, and retired rows drop out of List.
type memStore struct {
	rows    map[string]string // id -> tenant
	retired map[string]bool
	seq     int
}

func newMemStore() *memStore {
	return &memStore{rows: map[string]string{}, retired: map[string]bool{}}
}

func (m *memStore) create(tenant string) (string, error) {
	m.seq++
	id := "row-" + itoa(m.seq)
	m.rows[id] = tenant
	return id, nil
}

func (m *memStore) get(tenant, id string) error {
	if owner, ok := m.rows[id]; !ok || owner != tenant {
		return userstore.ErrNotFound
	}
	return nil
}

func (m *memStore) list(tenant string) ([]string, error) {
	var out []string
	for id, owner := range m.rows {
		if owner == tenant && !m.retired[id] {
			out = append(out, id)
		}
	}
	return out, nil
}

func (m *memStore) retire(tenant, id string) error {
	if owner, ok := m.rows[id]; !ok || owner != tenant {
		return userstore.ErrNotFound
	}
	m.retired[id] = true
	return nil
}

// updateKeepingTenant is the correct behaviour: the caller-supplied tenant is
// ignored and the stored one preserved.
func (m *memStore) updateKeepingTenant(tenant, id, _ string) error {
	return m.get(tenant, id)
}

func TestAssertListTenantScoped_CorrectStorePasses(t *testing.T) {
	m := newMemStore()
	contracttest.AssertListTenantScoped(t, contracttest.ListScopedStore{
		Create: func(_ context.Context, tid string) (string, error) { return m.create(tid) },
		List:   func(_ context.Context, tid string) ([]string, error) { return m.list(tid) },
	})
}

func TestAssertListTenantScoped_LeakyListFails(t *testing.T) {
	m := newMemStore()
	rec := &recordingTB{TB: t}
	func() {
		defer func() { _ = recover() }()
		contracttest.AssertListTenantScoped(rec, contracttest.ListScopedStore{
			Create: func(_ context.Context, tid string) (string, error) { return m.create(tid) },
			// Forgets the tenant predicate — returns every row.
			List: func(_ context.Context, _ string) ([]string, error) {
				var out []string
				for id := range m.rows {
					out = append(out, id)
				}
				return out, nil
			},
		})
	}()
	if !rec.failed {
		t.Fatal("helper did not catch a list that ignores the tenant predicate")
	}
}

func TestAssertUpdateCannotReassignTenant_CorrectStorePasses(t *testing.T) {
	m := newMemStore()
	contracttest.AssertUpdateCannotReassignTenant(t, contracttest.TenantImmutableStore{
		Create:            func(_ context.Context, tid string) (string, error) { return m.create(tid) },
		Update:            func(_ context.Context, tid, id string) error { return m.get(tid, id) },
		UpdateReassigning: func(_ context.Context, tid, id, newTID string) error { return m.updateKeepingTenant(tid, id, newTID) },
		Get:               func(_ context.Context, tid, id string) error { return m.get(tid, id) },
		NotFound:          userstore.ErrNotFound,
	})
}

func TestAssertUpdateCannotReassignTenant_ReassigningStoreFails(t *testing.T) {
	m := newMemStore()
	rec := &recordingTB{TB: t}
	func() {
		defer func() { _ = recover() }()
		contracttest.AssertUpdateCannotReassignTenant(rec, contracttest.TenantImmutableStore{
			Create: func(_ context.Context, tid string) (string, error) { return m.create(tid) },
			Update: func(_ context.Context, tid, id string) error { return m.get(tid, id) },
			// Writes the caller-supplied tenant — the row changes hands.
			UpdateReassigning: func(_ context.Context, _, id, newTID string) error {
				m.rows[id] = newTID
				return nil
			},
			Get:      func(_ context.Context, tid, id string) error { return m.get(tid, id) },
			NotFound: userstore.ErrNotFound,
		})
	}()
	if !rec.failed {
		t.Fatal("helper did not catch an Update that reassigns a row's tenant")
	}
}

func TestAssertRetiredHiddenFromList_CorrectStorePasses(t *testing.T) {
	m := newMemStore()
	contracttest.AssertRetiredHiddenFromList(t, contracttest.LifecycleStore{
		Create: func(_ context.Context, tid string) (string, error) { return m.create(tid) },
		Retire: func(_ context.Context, tid, id string) error { return m.retire(tid, id) },
		List:   func(_ context.Context, tid string) ([]string, error) { return m.list(tid) },
	})
}

func TestAssertRetiredHiddenFromList_ListingRetiredRowsFails(t *testing.T) {
	m := newMemStore()
	rec := &recordingTB{TB: t}
	func() {
		defer func() { _ = recover() }()
		contracttest.AssertRetiredHiddenFromList(rec, contracttest.LifecycleStore{
			Create: func(_ context.Context, tid string) (string, error) { return m.create(tid) },
			Retire: func(_ context.Context, tid, id string) error { return m.retire(tid, id) },
			// Ignores the retired flag — the exact defect shipped in v0.6.0.
			List: func(_ context.Context, tid string) ([]string, error) {
				var out []string
				for id, owner := range m.rows {
					if owner == tid {
						out = append(out, id)
					}
				}
				return out, nil
			},
		})
	}()
	if !rec.failed {
		t.Fatal("helper did not catch a List that keeps returning retired rows")
	}
}

// Codex's scenario: an adapter whose Update errors on everything previously
// "passed" the reassignment check by failing to do anything at all.
func TestAssertUpdateCannotReassignTenant_BrokenUpdateFails(t *testing.T) {
	m := newMemStore()
	rec := &recordingTB{TB: t}
	broken := errors.New("connection unavailable")
	func() {
		defer func() { _ = recover() }()
		contracttest.AssertUpdateCannotReassignTenant(rec, contracttest.TenantImmutableStore{
			Create:            func(_ context.Context, tid string) (string, error) { return m.create(tid) },
			Update:            func(_ context.Context, _, _ string) error { return broken },
			UpdateReassigning: func(_ context.Context, _, _, _ string) error { return broken },
			Get:               func(_ context.Context, tid, id string) error { return m.get(tid, id) },
			NotFound:          userstore.ErrNotFound,
		})
	}()
	if !rec.failed {
		t.Fatal("helper accepted a store whose Update never works — the reassignment check is vacuous")
	}
}
