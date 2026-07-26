// conformance_test.go runs the shared store conformance suite against the
// content_management sqlite adapter, so the list predicate and the
// tenant-immutability guarantee the Store contract states are held by an
// executable check rather than by review.
//
// ADR: ADR-0009 (ports-only module communication), ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package sqlite_test

// Validates: REQ-CONTENT-001, REQ-PORTS-001.
// Per: ADR-0009.
// Discipline: C-14.
import (
	"context"
	"testing"

	"github.com/septagon-oss/pk-modules/pkg/content/store"
	"github.com/septagon-oss/pk-modules/pkg/contracttest"
)

func TestSQLiteStoreConformance(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	seq := 0

	create := func(ctx context.Context, tenantID string) (string, error) {
		seq++
		id := "conf-" + itoa(seq)
		c := content(id, "conf-slug-"+itoa(seq))
		c.TenantID = tenantID
		return id, s.Create(ctx, c)
	}

	t.Run("list is tenant scoped", func(t *testing.T) {
		contracttest.AssertListTenantScoped(t, contracttest.ListScopedStore{
			Create: create,
			List: func(ctx context.Context, tenantID string) ([]string, error) {
				rows, err := s.List(ctx, tenantID, "", 100, 0)
				out := make([]string, 0, len(rows))
				for _, r := range rows {
					out = append(out, r.ID)
				}
				return out, err
			},
		})
	})

	// The Store contract states a row cannot be reassigned to another tenant
	// via Update. An adapter that writes the caller-supplied TenantID instead
	// of preserving the stored one hands a row to a different tenant.
	t.Run("update cannot reassign tenant", func(t *testing.T) {
		contracttest.AssertUpdateCannotReassignTenant(t, contracttest.TenantImmutableStore{
			Create: create,
			Update: func(ctx context.Context, tenantID, id string) error {
				existing, err := s.Get(ctx, tenantID, id)
				if err != nil {
					return err
				}
				existing.Title = "ordinary update"
				return s.Update(ctx, existing)
			},
			UpdateReassigning: func(ctx context.Context, tenantID, id, newTenantID string) error {
				existing, err := s.Get(ctx, tenantID, id)
				if err != nil {
					return err
				}
				existing.TenantID = newTenantID
				existing.Title = "reassignment attempt"
				return s.Update(ctx, existing)
			},
			Get: func(ctx context.Context, tenantID, id string) error {
				_, err := s.Get(ctx, tenantID, id)
				return err
			},
			NotFound: store.ErrNotFound,
		})
	})
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
