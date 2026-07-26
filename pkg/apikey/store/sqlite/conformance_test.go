// conformance_test.go runs the shared store conformance suite against the
// api_key_management sqlite adapter. Every store implementation of this
// contract — sqlite, Postgres, or a service-backed adapter — is expected to run
// the same suite, so a defect fixed in one backend cannot quietly survive in
// another.
//
// ADR: ADR-0009 (ports-only module communication), ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package sqlite_test

// Validates: REQ-APIKEY-001, REQ-PORTS-001.
// Per: ADR-0009.
// Discipline: C-14.
import (
	"context"
	"testing"

	"github.com/septagon-oss/pk-modules/pkg/apikey/store"
	"github.com/septagon-oss/pk-modules/pkg/contracttest"
)

func TestSQLiteStoreConformance(t *testing.T) {
	t.Parallel()

	create := func(s interface {
		Create(context.Context, *store.APIKey) error
	}, seq *int,
	) func(context.Context, string) (string, error) {
		return func(ctx context.Context, tenantID string) (string, error) {
			*seq++
			k := sampleKey("conf-" + itoa(*seq))
			k.TenantID = tenantID
			k.Prefix = "pk_conf" + itoa(*seq)
			return k.ID, s.Create(ctx, k)
		}
	}

	t.Run("list is tenant scoped", func(t *testing.T) {
		t.Parallel()
		s := newStore(t)
		seq := 0
		contracttest.AssertListTenantScoped(t, contracttest.ListScopedStore{
			Create: create(s, &seq),
			List: func(ctx context.Context, tenantID string) ([]string, error) {
				rows, err := s.List(ctx, tenantID)
				return ids(rows), err
			},
		})
	})

	// Revocation is the delete an operator performs, so a revoked key must stop
	// being listed. This is the defect fixed in v0.6.1, now held by a contract.
	t.Run("revoked keys are hidden from list", func(t *testing.T) {
		t.Parallel()
		s := newStore(t)
		seq := 0
		contracttest.AssertRetiredHiddenFromList(t, contracttest.LifecycleStore{
			Create: create(s, &seq),
			Retire: s.Revoke,
			List: func(ctx context.Context, tenantID string) ([]string, error) {
				rows, err := s.List(ctx, tenantID)
				return ids(rows), err
			},
		})
	})
}

func ids(rows []*store.APIKey) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.ID)
	}
	return out
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
