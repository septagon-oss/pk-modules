// Validates: REQ-PORTS-001.
// Per: ADR-0009.
// Discipline: C-14.

package migrate_test

// migrate_test.go exercises the runner on SQLite, which every behaviour here is
// engine-independent enough to prove. The Postgres-specific parts — the ledger's
// own dialect and the advisory lock — are covered by migrate_postgres_test.go
// against a real server.

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/septagon-oss/pk-modules/pkg/migrate"
)

func newDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+t.Name()+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	return db
}

var initial = []migrate.Migration{
	{Name: "0001_create_widgets", SQL: `CREATE TABLE IF NOT EXISTS widgets (id TEXT PRIMARY KEY)`},
}

func TestRunAppliesOnceAndIsIdempotent(t *testing.T) {
	db := newDB(t)
	ctx := context.Background()
	opts := migrate.Options{Module: "widget"}

	for range 3 {
		if err := migrate.Run(ctx, db, opts, initial); err != nil {
			t.Fatalf("run: %v", err)
		}
	}

	records, err := migrate.Status(ctx, db, false)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("ledger has %d rows after three runs, want 1", len(records))
	}
	if records[0].Module != "widget" || records[0].Name != "0001_create_widgets" {
		t.Fatalf("unexpected record: %+v", records[0])
	}
}

func TestRunAppliesPendingMigrationsInOrder(t *testing.T) {
	db := newDB(t)
	ctx := context.Background()
	opts := migrate.Options{Module: "widget"}

	if err := migrate.Run(ctx, db, opts, initial); err != nil {
		t.Fatalf("first release: %v", err)
	}

	// The second release adds a column — the thing the old CREATE-only scheme
	// could not express at all.
	next := append(append([]migrate.Migration{}, initial...),
		migrate.Migration{Name: "0002_add_label", SQL: `ALTER TABLE widgets ADD COLUMN label TEXT`})
	if err := migrate.Run(ctx, db, opts, next); err != nil {
		t.Fatalf("second release: %v", err)
	}

	if _, err := db.Exec(`INSERT INTO widgets (id, label) VALUES ('w1', 'hello')`); err != nil {
		t.Fatalf("new column not usable after migration: %v", err)
	}
	records, _ := migrate.Status(ctx, db, false)
	if len(records) != 2 {
		t.Fatalf("ledger has %d rows, want 2", len(records))
	}
}

// TestAdoptsAnExistingDatabase is the upgrade path for every database created
// before this package existed: the table is already there, so migration 0001 is
// a no-op that simply gets recorded.
func TestAdoptsAnExistingDatabase(t *testing.T) {
	db := newDB(t)
	ctx := context.Background()
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS widgets (id TEXT PRIMARY KEY)`); err != nil {
		t.Fatalf("pre-existing schema: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO widgets (id) VALUES ('pre-existing')`); err != nil {
		t.Fatalf("pre-existing row: %v", err)
	}

	if err := migrate.Run(ctx, db, migrate.Options{Module: "widget"}, initial); err != nil {
		t.Fatalf("adopting an existing database: %v", err)
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM widgets`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("pre-existing data lost: count=%d err=%v", count, err)
	}
}

func TestEditedMigrationIsRefused(t *testing.T) {
	db := newDB(t)
	ctx := context.Background()
	opts := migrate.Options{Module: "widget"}

	if err := migrate.Run(ctx, db, opts, initial); err != nil {
		t.Fatalf("run: %v", err)
	}

	tampered := []migrate.Migration{
		{Name: "0001_create_widgets", SQL: `CREATE TABLE IF NOT EXISTS widgets (id TEXT PRIMARY KEY, sneaky TEXT)`},
	}
	err := migrate.Run(ctx, db, opts, tampered)
	if !errors.Is(err, migrate.ErrDrift) {
		t.Fatalf("editing an applied migration = %v, want ErrDrift", err)
	}
	if !strings.Contains(err.Error(), "0001_create_widgets") {
		t.Errorf("error should name the migration: %v", err)
	}
}

// TestDowngradeIsRefused covers the rollback case: a database migrated by a
// newer build, then served by an older one.
func TestDowngradeIsRefused(t *testing.T) {
	db := newDB(t)
	ctx := context.Background()
	opts := migrate.Options{Module: "widget"}

	newer := append(append([]migrate.Migration{}, initial...),
		migrate.Migration{Name: "0002_add_label", SQL: `ALTER TABLE widgets ADD COLUMN label TEXT`})
	if err := migrate.Run(ctx, db, opts, newer); err != nil {
		t.Fatalf("newer build: %v", err)
	}

	err := migrate.Run(ctx, db, opts, initial) // the older build
	if !errors.Is(err, migrate.ErrUnknownMigration) {
		t.Fatalf("older build over a migrated database = %v, want ErrUnknownMigration", err)
	}
}

func TestModulesDoNotShareANamespace(t *testing.T) {
	db := newDB(t)
	ctx := context.Background()

	// Both modules ship a migration called 0001_init. Neither may disturb the
	// other, and neither may see the other's history.
	a := []migrate.Migration{{Name: "0001_init", SQL: `CREATE TABLE IF NOT EXISTS alpha (id TEXT)`}}
	b := []migrate.Migration{{Name: "0001_init", SQL: `CREATE TABLE IF NOT EXISTS beta (id TEXT)`}}

	if err := migrate.Run(ctx, db, migrate.Options{Module: "alpha"}, a); err != nil {
		t.Fatalf("alpha: %v", err)
	}
	if err := migrate.Run(ctx, db, migrate.Options{Module: "beta"}, b); err != nil {
		t.Fatalf("beta: %v", err)
	}
	// Re-running each must still be a no-op, not a drift error from the other's
	// identically named migration.
	if err := migrate.Run(ctx, db, migrate.Options{Module: "alpha"}, a); err != nil {
		t.Fatalf("alpha rerun: %v", err)
	}

	records, _ := migrate.Status(ctx, db, false)
	if len(records) != 2 {
		t.Fatalf("ledger has %d rows, want 2 (one per module)", len(records))
	}
}

func TestMalformedMigrationSetsAreRejected(t *testing.T) {
	db := newDB(t)
	ctx := context.Background()
	opts := migrate.Options{Module: "widget"}

	cases := map[string][]migrate.Migration{
		"unordered": {
			{Name: "0002_b", SQL: "SELECT 1"},
			{Name: "0001_a", SQL: "SELECT 1"},
		},
		"duplicate": {
			{Name: "0001_a", SQL: "SELECT 1"},
			{Name: "0001_a", SQL: "SELECT 2"},
		},
		"unnamed":   {{Name: "  ", SQL: "SELECT 1"}},
		"empty sql": {{Name: "0001_a", SQL: ""}},
	}
	for name, set := range cases {
		if err := migrate.Run(ctx, db, opts, set); err == nil {
			t.Errorf("%s migration set was accepted", name)
		}
	}
	if err := migrate.Run(ctx, db, migrate.Options{}, initial); err == nil {
		t.Error("a migration run without a module namespace was accepted")
	}
}

// TestFailedMigrationLeavesNothingBehind proves the transaction: a migration
// whose second statement fails must not leave the first one applied, and must
// not be recorded.
func TestFailedMigrationLeavesNothingBehind(t *testing.T) {
	db := newDB(t)
	ctx := context.Background()
	opts := migrate.Options{Module: "widget"}

	broken := []migrate.Migration{{
		Name: "0001_two_statements",
		SQL: `CREATE TABLE IF NOT EXISTS good (id TEXT);
		      CREATE TABLE good (id TEXT);`, // second statement fails: already exists
	}}
	if err := migrate.Run(ctx, db, opts, broken); err == nil {
		t.Fatal("a failing migration reported success")
	}

	var name string
	err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name='good'`).Scan(&name)
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("partial migration left table %q behind (err=%v)", name, err)
	}
	records, _ := migrate.Status(ctx, db, false)
	if len(records) != 0 {
		t.Errorf("failed migration was recorded: %+v", records)
	}
}

// TestConcurrentRunsApplyOnce guards the case a rollout creates: several
// processes running the same migrations at once.
func TestConcurrentRunsApplyOnce(t *testing.T) {
	db := newDB(t)
	ctx := context.Background()
	opts := migrate.Options{Module: "widget"}

	var wg sync.WaitGroup
	errs := make([]error, 8)
	for i := range errs {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			errs[n] = migrate.Run(ctx, db, opts, initial)
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Errorf("concurrent run %d: %v", i, err)
		}
	}
	records, _ := migrate.Status(ctx, db, false)
	if len(records) != 1 {
		t.Fatalf("ledger has %d rows after concurrent runs, want 1", len(records))
	}
}
