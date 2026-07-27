// Validates: REQ-PORTS-001.
// Per: ADR-0009.
// Discipline: C-14.

package migrate_test

// migrate_postgres_test.go runs the engine-specific half against a real
// Postgres: the ledger's own dialect, and the behaviours that matter most on
// the production profile — adopting a database created before this package
// existed, and refusing drift. Gated on PK_POSTGRES_TEST_DSN so a machine
// without Postgres still runs a green suite.

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strings"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/septagon-oss/pk-modules/pkg/migrate"
)

func newPG(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("PK_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("PK_POSTGRES_TEST_DSN not set; skipping Postgres migration tests")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	// Clean only this test's own artifacts. Every Postgres test package in this
	// repository shares one database, so dropping the schema would delete
	// tables a sibling package is using — these run in parallel by default.
	for _, stmt := range []string{
		`DROP TABLE IF EXISTS widgets`,
		`DROP TABLE IF EXISTS alpha`,
		`DROP TABLE IF EXISTS beta`,
		`DELETE FROM ` + migrate.LedgerTable + ` WHERE module IN ('widget','alpha','beta')`,
	} {
		if _, err := db.Exec(stmt); err != nil && !strings.Contains(err.Error(), "does not exist") {
			t.Fatalf("reset (%s): %v", stmt, err)
		}
	}
	return db
}

// forModule filters the ledger to one module, so an assertion about this
// test's migrations is unaffected by whatever else shares the database.
func forModule(records []migrate.Record, module string) []migrate.Record {
	var out []migrate.Record
	for _, r := range records {
		if r.Module == module {
			out = append(out, r)
		}
	}
	return out
}

var pgInitial = []migrate.Migration{
	{Name: "0001_create_widgets", SQL: `CREATE TABLE IF NOT EXISTS widgets (
		id TEXT PRIMARY KEY,
		created_at TIMESTAMPTZ NOT NULL DEFAULT now()
	)`},
}

func TestPostgresRunAppliesAndEvolves(t *testing.T) {
	db := newPG(t)
	ctx := context.Background()
	opts := migrate.Options{Module: "widget", Postgres: true}

	if err := migrate.Run(ctx, db, opts, pgInitial); err != nil {
		t.Fatalf("first release: %v", err)
	}
	// Re-running is a no-op.
	if err := migrate.Run(ctx, db, opts, pgInitial); err != nil {
		t.Fatalf("second run: %v", err)
	}

	// The change the old CREATE-only scheme could not express.
	next := append(append([]migrate.Migration{}, pgInitial...),
		migrate.Migration{Name: "0002_add_label", SQL: `ALTER TABLE widgets ADD COLUMN label TEXT`})
	if err := migrate.Run(ctx, db, opts, next); err != nil {
		t.Fatalf("schema evolution: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO widgets (id, label) VALUES ('w1', 'hello')`); err != nil {
		t.Fatalf("new column unusable: %v", err)
	}

	all, err := migrate.Status(ctx, db, true)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	records := forModule(all, "widget")
	if len(records) != 2 {
		t.Fatalf("ledger has %d rows for widget, want 2", len(records))
	}
	if records[0].AppliedAt.IsZero() {
		t.Error("applied_at did not round-trip through TIMESTAMPTZ")
	}
}

// TestPostgresAdoptsAnExistingDatabase is the upgrade every current deployment
// takes: tables already exist, no ledger does.
func TestPostgresAdoptsAnExistingDatabase(t *testing.T) {
	db := newPG(t)
	ctx := context.Background()

	if _, err := db.Exec(pgInitial[0].SQL); err != nil {
		t.Fatalf("pre-existing schema: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO widgets (id) VALUES ('pre-existing')`); err != nil {
		t.Fatalf("pre-existing row: %v", err)
	}

	if err := migrate.Run(ctx, db, migrate.Options{Module: "widget", Postgres: true}, pgInitial); err != nil {
		t.Fatalf("adoption: %v", err)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM widgets`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("pre-existing data lost: count=%d err=%v", count, err)
	}
}

func TestPostgresRefusesDriftAndDowngrade(t *testing.T) {
	db := newPG(t)
	ctx := context.Background()
	opts := migrate.Options{Module: "widget", Postgres: true}

	next := append(append([]migrate.Migration{}, pgInitial...),
		migrate.Migration{Name: "0002_add_label", SQL: `ALTER TABLE widgets ADD COLUMN label TEXT`})
	if err := migrate.Run(ctx, db, opts, next); err != nil {
		t.Fatalf("setup: %v", err)
	}

	if err := migrate.Run(ctx, db, opts, pgInitial); !errors.Is(err, migrate.ErrUnknownMigration) {
		t.Errorf("downgrade = %v, want ErrUnknownMigration", err)
	}
	tampered := []migrate.Migration{{Name: "0001_create_widgets", SQL: `CREATE TABLE IF NOT EXISTS widgets (id TEXT)`}}
	if err := migrate.Run(ctx, db, opts, tampered); !errors.Is(err, migrate.ErrDrift) {
		t.Errorf("edited migration = %v, want ErrDrift", err)
	}
}
