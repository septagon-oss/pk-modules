// Validates: REQ-PORTS-001.
// Per: ADR-0009.
// Discipline: C-14.
package contracttest_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/septagon-oss/pk-modules/pkg/contracttest"
	userstore "github.com/septagon-oss/pk-modules/pkg/user/store"
	usersqlite "github.com/septagon-oss/pk-modules/pkg/user/store/sqlite"

	_ "modernc.org/sqlite"
)

// TestAssertTenantScoped_RealStorePasses drives the helper against a genuine
// tenant-scoped store (the built-in user store). It must pass — proving the
// helper accepts a correct implementation.
func TestAssertTenantScoped_RealStorePasses(t *testing.T) {
	db, err := sql.Open("sqlite", "file:contracttest_ok?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s, err := usersqlite.New(db)
	if err != nil {
		t.Fatal(err)
	}

	n := 0
	contracttest.AssertTenantScoped(t, contracttest.TenantScopedStore{
		Create: func(ctx context.Context, tid string) (string, error) {
			n++
			u := &userstore.User{
				ID:       "u-" + tid + "-" + itoa(n),
				TenantID: tid,
				Email:    "user" + itoa(n) + "@" + tid + ".test",
				Username: "user-" + tid + "-" + itoa(n),
				Active:   true,
			}
			return u.ID, s.Create(ctx, u)
		},
		Get:      func(ctx context.Context, tid, id string) error { _, err := s.Get(ctx, tid, id); return err },
		Delete:   s.Delete,
		NotFound: userstore.ErrNotFound,
	})
}

// leakyStore forgets the tenant predicate on Get and Delete — the exact bug the
// helper exists to catch.
type leakyStore struct{ rows map[string]string } // id -> tenant

func (l *leakyStore) create(tid string) (string, error) {
	id := "id-" + itoa(len(l.rows)+1)
	l.rows[id] = tid
	return id, nil
}
func (l *leakyStore) getNoTenant(id string) error {
	if _, ok := l.rows[id]; ok {
		return nil // BUG: ignores tenant
	}
	return userstore.ErrNotFound
}
func (l *leakyStore) deleteNoTenant(id string) error {
	if _, ok := l.rows[id]; ok {
		delete(l.rows, id) // BUG: ignores tenant
		return nil
	}
	return userstore.ErrNotFound
}

// TestAssertTenantScoped_LeakyStoreFails proves the helper CATCHES a store that
// forgets the tenant predicate — it must report a failure, not pass silently.
func TestAssertTenantScoped_LeakyStoreFails(t *testing.T) {
	l := &leakyStore{rows: map[string]string{}}
	rec := &recordingTB{TB: t}
	func() {
		defer func() { _ = recover() }() // the recorded Fatalf panics to stop; swallow it
		contracttest.AssertTenantScoped(rec, contracttest.TenantScopedStore{
			Create:   func(ctx context.Context, tid string) (string, error) { return l.create(tid) },
			Get:      func(ctx context.Context, tid, id string) error { return l.getNoTenant(id) },
			Delete:   func(ctx context.Context, tid, id string) error { return l.deleteNoTenant(id) },
			NotFound: userstore.ErrNotFound,
		})
	}()
	if !rec.failed {
		t.Fatal("helper did not catch a tenant-leaking store — the check is toothless")
	}
}

// recordingTB captures a Fatal instead of aborting, so the negative test can
// assert that a failure was reported.
type recordingTB struct {
	testing.TB
	failed bool
}

func (r *recordingTB) Helper()                        {}
func (r *recordingTB) Fatalf(string, ...any)          { r.failed = true; panic(sentinel{}) }
func (r *recordingTB) Fatal(...any)                   { r.failed = true; panic(sentinel{}) }
func (r *recordingTB) Errorf(format string, a ...any) { r.failed = true }
func (r *recordingTB) Error(a ...any)                 { r.failed = true }

type sentinel struct{}

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
