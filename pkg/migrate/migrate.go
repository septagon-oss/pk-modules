// Implements: REQ-PORTS-001.
// Per: ADR-0009, ADR-0029.
// Discipline: C-14.

// Package migrate gives modules a way to evolve their schema.
//
// Until now a store created its tables with one CREATE TABLE IF NOT EXISTS
// blob at construction. That is fine exactly once. It cannot add a column,
// backfill a value, or split a table, and it leaves no record of what a given
// database has actually had done to it — so the second release of any module
// had nowhere to put its change. This package is that place.
//
// The model is deliberately small:
//
//   - A migration is an ordered, immutable pair of a name and some SQL. It is
//     applied once, in name order, inside a transaction.
//   - Every module owns its own migrations and its own namespace in one shared
//     ledger, so "what has this database had done to it" is a single query and
//     modules still cannot reach into each other's history.
//   - Applied migrations are checksummed. Editing SQL that has already run
//     somewhere is the defining schema-drift bug — two databases silently
//     disagree while both look migrated — so it fails loudly instead.
//
// It fails closed in both directions. A checksum that no longer matches stops
// the boot, and so does a database carrying a migration this binary has never
// heard of: that is a downgrade, and code from before a schema change cannot be
// assumed to survive contact with the schema after it.
//
// Concurrency is handled here as well as by any caller, because a migration is
// a check-then-act and DDL is not generally safe to run twice: two runners that
// both read an empty ledger would both try to ALTER the same table.
//
//   - Within a process, a mutex serializes runs on every engine.
//   - Across processes on Postgres, an advisory lock does. It uses its own key,
//     so a caller already holding a broader boot lock does not deadlock against
//     it. This is not theoretical: CREATE TABLE IF NOT EXISTS races in the
//     Postgres system catalog and loses.
//   - Across processes on SQLite is out of scope, and is not a gap: the
//     embedded profile is one process over one file by definition.
//
// Each migration then re-checks the ledger inside its own transaction, so even
// a caller that defeats the above applies nothing twice.
package migrate

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// inProcess serializes migration runs inside one process on every engine. The
// Postgres advisory lock covers other processes; this covers goroutines, which
// no database lock can see.
var inProcess sync.Mutex

// LedgerTable records every applied migration. The name is deliberately
// prefixed: `schema_migrations` unqualified is what golang-migrate and several
// other tools claim, and a downstream application is entitled to use one of
// those alongside this one without the two fighting over a table.
const LedgerTable = "pk_schema_migrations"

// lockKey serializes migration runs on Postgres. It differs from any lock a
// caller may already hold — the starter takes a broader boot-wide lock — so
// holding both is safe as long as the broader one is always taken first, which
// it is: callers reach this code from inside their own boot.
const lockKey int64 = 0x706b_6d_69_67_72_61_74

// Migration is one immutable schema change.
//
// Name orders the migration and identifies it in the ledger forever, so the
// convention is a zero-padded sequence and a description: "0002_add_user_locale".
// SQL may contain several statements; the whole migration runs in one
// transaction, so a failure leaves nothing half-applied.
type Migration struct {
	Name string
	SQL  string
}

// Record is one row of the ledger.
type Record struct {
	Module    string
	Name      string
	Checksum  string
	AppliedAt time.Time
}

// Options describes the caller.
type Options struct {
	// Module namespaces the migrations. Two modules may both ship "0001_init"
	// without collision, and no module can see another's history.
	Module string
	// Postgres selects the dialect for the ledger's own DDL and enables the
	// advisory lock. Everything else is the caller's SQL, unchanged.
	Postgres bool
}

// ErrDrift reports SQL that changed after it had already been applied.
var ErrDrift = errors.New("migrate: applied migration changed")

// ErrUnknownMigration reports a database migrated past this binary.
var ErrUnknownMigration = errors.New("migrate: database has a migration this build does not know")

// Run applies whatever of migrations this database has not seen, in order.
// It is safe to call on every boot and safe to call concurrently.
func Run(ctx context.Context, db *sql.DB, opts Options, migrations []Migration) (err error) {
	if db == nil {
		return errors.New("migrate: nil database")
	}
	if strings.TrimSpace(opts.Module) == "" {
		return errors.New("migrate: Options.Module is required to namespace the ledger")
	}
	if err := validate(migrations); err != nil {
		return err
	}

	inProcess.Lock()
	defer inProcess.Unlock()

	unlock, err := lock(ctx, db, opts.Postgres)
	if err != nil {
		return err
	}
	// A failed unlock leaves a pooled session holding the advisory lock and
	// would block every future run, so it fails the run unless an earlier
	// error already explains the state.
	defer func() {
		if uerr := unlock(); uerr != nil && err == nil {
			err = uerr
		}
	}()

	if err := ensureLedger(ctx, db, opts.Postgres); err != nil {
		return err
	}
	applied, err := appliedFor(ctx, db, opts)
	if err != nil {
		return err
	}
	if err := reconcile(opts.Module, migrations, applied); err != nil {
		return err
	}

	for _, m := range migrations {
		if _, done := applied[m.Name]; done {
			continue
		}
		if err := apply(ctx, db, opts, m); err != nil {
			return err
		}
	}
	return nil
}

// Status returns the ledger, newest first, across every module. It is what a
// `migrate status` command reports and what an operator reads during an
// incident.
func Status(ctx context.Context, db *sql.DB, postgres bool) ([]Record, error) {
	if db == nil {
		return nil, errors.New("migrate: nil database")
	}
	if err := ensureLedger(ctx, db, postgres); err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx,
		`SELECT module, name, checksum, applied_at FROM `+LedgerTable+
			` ORDER BY applied_at DESC, module ASC, name ASC`)
	if err != nil {
		return nil, fmt.Errorf("migrate: read ledger: %w", err)
	}
	defer rows.Close()

	var out []Record
	for rows.Next() {
		var r Record
		if err := rows.Scan(&r.Module, &r.Name, &r.Checksum, &r.AppliedAt); err != nil {
			return nil, fmt.Errorf("migrate: scan ledger: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// validate rejects a migration set that cannot be applied deterministically:
// empty names, duplicates, or an order the caller did not actually declare.
func validate(migrations []Migration) error {
	seen := map[string]bool{}
	for i, m := range migrations {
		if strings.TrimSpace(m.Name) == "" {
			return fmt.Errorf("migrate: migration %d has no name", i)
		}
		if strings.TrimSpace(m.SQL) == "" {
			return fmt.Errorf("migrate: migration %q has no SQL", m.Name)
		}
		if seen[m.Name] {
			return fmt.Errorf("migrate: duplicate migration name %q", m.Name)
		}
		seen[m.Name] = true
		if i > 0 && migrations[i-1].Name >= m.Name {
			return fmt.Errorf("migrate: migration %q is not ordered after %q; "+
				"names order the run and must ascend", m.Name, migrations[i-1].Name)
		}
	}
	return nil
}

// reconcile compares what the database has against what this build ships.
func reconcile(module string, migrations []Migration, applied map[string]Record) error {
	shipped := make(map[string]Migration, len(migrations))
	for _, m := range migrations {
		shipped[m.Name] = m
	}
	names := make([]string, 0, len(applied))
	for name := range applied {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		record := applied[name]
		m, ok := shipped[name]
		if !ok {
			return fmt.Errorf("%w: module %q has %q applied; this build ships only up to %q. "+
				"Deploying an older build over a migrated database risks writing rows the newer "+
				"schema rejects, so it stops here rather than guessing",
				ErrUnknownMigration, module, name, lastName(migrations))
		}
		if sum := checksum(m.SQL); sum != record.Checksum {
			return fmt.Errorf("%w: module %q migration %q was applied as %s but now hashes %s. "+
				"A migration is immutable once released — every database that already ran the old "+
				"text would silently differ from one running the new. Ship a new migration instead",
				ErrDrift, module, name, short(record.Checksum), short(sum))
		}
	}
	return nil
}

func apply(ctx context.Context, db *sql.DB, opts Options, m Migration) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("migrate: begin %q: %w", m.Name, err)
	}
	defer func() { _ = tx.Rollback() }() // justified: after a successful Commit this Rollback returns sql.ErrTxDone by design; on failure paths the original apply/record error is already propagated

	// Re-check under the transaction. The locks above should make this
	// impossible to hit, which is exactly why it is here: if some caller
	// defeats them, the cost of being wrong is running DDL twice.
	exists := `SELECT COUNT(*) FROM ` + LedgerTable + ` WHERE module = ? AND name = ?`
	if opts.Postgres {
		exists = `SELECT COUNT(*) FROM ` + LedgerTable + ` WHERE module = $1 AND name = $2`
	}
	var already int
	if err := tx.QueryRowContext(ctx, exists, opts.Module, m.Name).Scan(&already); err != nil {
		return fmt.Errorf("migrate: re-check %s/%s: %w", opts.Module, m.Name, err)
	}
	if already != 0 {
		return nil
	}

	if _, err := tx.ExecContext(ctx, m.SQL); err != nil {
		return fmt.Errorf("migrate: apply %s/%s: %w", opts.Module, m.Name, err)
	}
	insert := `INSERT INTO ` + LedgerTable + ` (module, name, checksum, applied_at) VALUES (?, ?, ?, ?)`
	if opts.Postgres {
		insert = `INSERT INTO ` + LedgerTable + ` (module, name, checksum, applied_at) VALUES ($1, $2, $3, $4)`
	}
	if _, err := tx.ExecContext(ctx, insert, opts.Module, m.Name, checksum(m.SQL), time.Now().UTC()); err != nil {
		return fmt.Errorf("migrate: record %s/%s: %w", opts.Module, m.Name, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("migrate: commit %s/%s: %w", opts.Module, m.Name, err)
	}
	return nil
}

func appliedFor(ctx context.Context, db *sql.DB, opts Options) (map[string]Record, error) {
	query := `SELECT module, name, checksum, applied_at FROM ` + LedgerTable + ` WHERE module = ?`
	if opts.Postgres {
		query = `SELECT module, name, checksum, applied_at FROM ` + LedgerTable + ` WHERE module = $1`
	}
	rows, err := db.QueryContext(ctx, query, opts.Module)
	if err != nil {
		return nil, fmt.Errorf("migrate: read ledger for %q: %w", opts.Module, err)
	}
	defer rows.Close()

	out := map[string]Record{}
	for rows.Next() {
		var r Record
		if err := rows.Scan(&r.Module, &r.Name, &r.Checksum, &r.AppliedAt); err != nil {
			return nil, fmt.Errorf("migrate: scan ledger: %w", err)
		}
		out[r.Name] = r
	}
	return out, rows.Err()
}

func ensureLedger(ctx context.Context, db *sql.DB, postgres bool) error {
	timestamp := "DATETIME"
	if postgres {
		timestamp = "TIMESTAMPTZ"
	}
	ddl := `CREATE TABLE IF NOT EXISTS ` + LedgerTable + ` (
		module TEXT NOT NULL,
		name TEXT NOT NULL,
		checksum TEXT NOT NULL,
		applied_at ` + timestamp + ` NOT NULL,
		PRIMARY KEY (module, name)
	)`
	if _, err := db.ExecContext(ctx, ddl); err != nil {
		return fmt.Errorf("migrate: ensure ledger: %w", err)
	}
	return nil
}

// lock serializes concurrent migration runs on Postgres. SQLite is a
// single-writer engine and needs no equivalent.
//
// The returned unlock reports its own failure: Conn.Close returns the session
// to the pool rather than tearing it down, so a failed pg_advisory_unlock can
// leave a pooled session holding the lock and silently blocking every future
// migration run. That is worth surfacing to the caller.
func lock(ctx context.Context, db *sql.DB, postgres bool) (func() error, error) {
	if !postgres {
		return func() error { return nil }, nil
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("migrate: connection for lock: %w", err)
	}
	if _, err := conn.ExecContext(ctx, `SELECT pg_advisory_lock($1)`, lockKey); err != nil {
		_ = conn.Close() // justified: cleanup on the lock-acquisition failure path; the acquire error below is the actionable one and is returned
		return nil, fmt.Errorf("migrate: acquire lock: %w", err)
	}
	return func() error {
		var uerr error
		if _, err := conn.ExecContext(ctx, `SELECT pg_advisory_unlock($1)`, lockKey); err != nil {
			uerr = fmt.Errorf("migrate: release lock: %w", err)
		}
		if cerr := conn.Close(); cerr != nil && uerr == nil {
			uerr = fmt.Errorf("migrate: close lock connection: %w", cerr)
		}
		return uerr
	}, nil
}

func checksum(sql string) string {
	sum := sha256.Sum256([]byte(sql))
	return hex.EncodeToString(sum[:])
}

func short(sum string) string {
	if len(sum) > 12 {
		return sum[:12]
	}
	return sum
}

func lastName(migrations []Migration) string {
	if len(migrations) == 0 {
		return "(none)"
	}
	return migrations[len(migrations)-1].Name
}
