// postgres_test.go runs the audit_management Postgres adapter through the same
// tenant-scoping conformance check the sqlite adapter is held to, plus the
// append-only round-trip, filter, ordering and paging behaviour, against a real
// Postgres. It is gated on PK_POSTGRES_TEST_DSN: with no DSN the tests skip, so
// `go test ./...` stays green on a machine without Postgres, and CI (or a
// developer with a container) sets the DSN to enforce the contract. The point of
// the pivot: the production adapter is held to the identical tenant-isolation
// guarantees as the embedded one, by executable check rather than by faith.
//
// ADR: ADR-0009 (ports-only module communication), ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).
package postgres_test

// Validates: REQ-AUDIT-001, REQ-PORTS-001.
// Per: ADR-0009.
// Discipline: C-14.
import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/septagon-oss/pk-modules/pkg/audit/store"
	"github.com/septagon-oss/pk-modules/pkg/audit/store/postgres"
	"github.com/septagon-oss/pk-modules/pkg/contracttest"
)

// dsn returns the Postgres test DSN or skips the test. Example:
//
//	PK_POSTGRES_TEST_DSN='postgres://postgres:pk@127.0.0.1:55432/pktest?sslmode=disable'
func dsn(t *testing.T) string {
	t.Helper()
	v := os.Getenv("PK_POSTGRES_TEST_DSN")
	if v == "" {
		t.Skip("PK_POSTGRES_TEST_DSN not set; skipping Postgres conformance")
	}
	return v
}

// newStore opens the adapter and truncates the table so each test starts clean.
// The tests are serial (no t.Parallel) because they share one database.
func newStore(t *testing.T) *postgres.Store {
	t.Helper()
	db, err := sql.Open("pgx", dsn(t))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	s, err := postgres.New(db)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	if _, err := db.Exec(`TRUNCATE TABLE audit_events`); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	return s
}

func event(id string, at time.Time) *store.Event {
	return &store.Event{
		ID:        id,
		TenantID:  "tenant-1",
		Actor:     "actor-1",
		Action:    "thing.done",
		Resource:  "res-1",
		Severity:  "info",
		Details:   `{"k":"v"}`,
		EmittedAt: at,
	}
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

func TestPostgresStoreConformance(t *testing.T) {
	s := newStore(t)
	seq := 0
	create := func(ctx context.Context, tenantID string) (string, error) {
		seq++
		id := "conf-" + itoa(seq)
		e := event(id, time.Time{})
		e.TenantID = tenantID
		return id, s.Append(ctx, e)
	}

	// The append-only surface has no Get/Delete/Update, so the applicable
	// invariant is that Query never crosses the tenant boundary.
	t.Run("query is tenant scoped", func(t *testing.T) {
		contracttest.AssertListTenantScoped(t, contracttest.ListScopedStore{
			Create: create,
			List: func(ctx context.Context, tenantID string) ([]string, error) {
				rows, err := s.Query(ctx, store.QueryFilter{TenantID: tenantID})
				out := make([]string, 0, len(rows))
				for _, r := range rows {
					out = append(out, r.ID)
				}
				return out, err
			},
		})
	})
}

func TestPostgresStoreAppendThenQuery(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	e := event("e1", time.Time{}) // zero EmittedAt -> defaulted
	if err := s.Append(ctx, e); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if e.EmittedAt.IsZero() {
		t.Fatal("Append should default EmittedAt")
	}

	got, err := s.Query(ctx, store.QueryFilter{TenantID: "tenant-1"})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("Query returned %d events, want 1", len(got))
	}
	if got[0].ID != "e1" || got[0].Details != `{"k":"v"}` {
		t.Fatalf("round-trip mismatch: %+v", got[0])
	}
}

func TestPostgresStoreAppendDuplicateID(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	if err := s.Append(ctx, event("dup", now)); err != nil {
		t.Fatalf("Append #1: %v", err)
	}
	err := s.Append(ctx, event("dup", now))
	if !errors.Is(err, store.ErrDuplicateID) {
		t.Fatalf("Append duplicate err = %v, want ErrDuplicateID", err)
	}
}

func TestPostgresStoreQueryOrdersChronologically(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	// Insert out of order; expect ascending emitted_at.
	if err := s.Append(ctx, event("c", base.Add(2*time.Hour))); err != nil {
		t.Fatal(err)
	}
	if err := s.Append(ctx, event("a", base)); err != nil {
		t.Fatal(err)
	}
	if err := s.Append(ctx, event("b", base.Add(time.Hour))); err != nil {
		t.Fatal(err)
	}
	got, err := s.Query(ctx, store.QueryFilter{TenantID: "tenant-1"})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	want := []string{"a", "b", "c"}
	if len(got) != len(want) {
		t.Fatalf("got %d events, want %d", len(got), len(want))
	}
	for i, id := range want {
		if got[i].ID != id {
			t.Fatalf("order[%d] = %s, want %s", i, got[i].ID, id)
		}
	}
}

func TestPostgresStoreQueryFilters(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	base := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)

	mk := func(id, tenant, actor, action string, at time.Time) *store.Event {
		e := event(id, at)
		e.TenantID, e.Actor, e.Action = tenant, actor, action
		return e
	}
	seed := []*store.Event{
		mk("1", "t1", "alice", "login", base),
		mk("2", "t1", "bob", "logout", base.Add(time.Hour)),
		mk("3", "t2", "alice", "login", base.Add(2*time.Hour)),
	}
	for _, e := range seed {
		if err := s.Append(ctx, e); err != nil {
			t.Fatalf("Append %s: %v", e.ID, err)
		}
	}

	// Every query is tenant-scoped: a filter must name a tenant, and results
	// never cross the tenant boundary. Counts are within-tenant.
	cases := []struct {
		name   string
		filter store.QueryFilter
		wantN  int
	}{
		{"tenant t1", store.QueryFilter{TenantID: "t1"}, 2},
		{"tenant t2", store.QueryFilter{TenantID: "t2"}, 1},
		{"actor within t1", store.QueryFilter{TenantID: "t1", Actor: "alice"}, 1},
		{"action within t1", store.QueryFilter{TenantID: "t1", Action: "login"}, 1},
		{"since within t1", store.QueryFilter{TenantID: "t1", Since: base.Add(time.Hour)}, 1},
		{"until within t1", store.QueryFilter{TenantID: "t1", Until: base.Add(time.Hour)}, 1},
		{"window within t1", store.QueryFilter{TenantID: "t1", Since: base.Add(time.Hour), Until: base.Add(2 * time.Hour)}, 1},
		{"none-match within t1", store.QueryFilter{TenantID: "t1", Actor: "carol"}, 0},
		// Cross-tenant isolation: t2's actor "alice" event is invisible to t1.
		{"other tenant actor invisible", store.QueryFilter{TenantID: "t1", Actor: "alice", Action: "login"}, 1},
		// Fail closed: an unscoped query returns nothing, never the whole log.
		{"empty tenant fails closed", store.QueryFilter{}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := s.Query(ctx, tc.filter)
			if err != nil {
				t.Fatalf("Query: %v", err)
			}
			if len(got) != tc.wantN {
				t.Fatalf("got %d events, want %d", len(got), tc.wantN)
			}
		})
	}
}

func TestPostgresStoreQueryLimitCap(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	base := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	for i := range 5 {
		if err := s.Append(ctx, event(string(rune('a'+i)), base.Add(time.Duration(i)*time.Minute))); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
	got, err := s.Query(ctx, store.QueryFilter{TenantID: "tenant-1", Limit: 2})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("Query(limit=2) returned %d, want 2", len(got))
	}
	page, err := s.Query(ctx, store.QueryFilter{TenantID: "tenant-1", Limit: 2, Offset: 2})
	if err != nil {
		t.Fatalf("Query(offset=2): %v", err)
	}
	if len(page) != 2 || page[0].ID != "c" || page[1].ID != "d" {
		t.Fatalf("Query(limit=2, offset=2) = %+v, want events c and d", page)
	}
}

func TestPostgresStoreAppendNil(t *testing.T) {
	s := newStore(t)
	if err := s.Append(context.Background(), nil); err == nil {
		t.Fatal("Append(nil) should return an error")
	}
}

func TestNewRejectsNilDB(t *testing.T) {
	if _, err := postgres.New(nil); err == nil {
		t.Fatal("New(nil) should return an error")
	}
}
