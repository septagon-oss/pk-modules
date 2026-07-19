// Implements: REQ-TENANT-001.
// Per: ADR-0017.
// Discipline: C-14.

package tenant

// options.go owns functional options used by NewModule. New options should be
// purely additive; never change the meaning of an existing option.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

import (
	"github.com/septagon-oss/pk-modules/pkg/portslib"
	"github.com/septagon-oss/pk-modules/pkg/tenant/store"
)

// Option configures a Module at construction time.
type Option func(*config)

type config struct {
	store        store.Store
	admin        portslib.AdminRegistrar
	health       portslib.HealthRegistrar
	sqliteDSN    string
	sqliteDriver string
}

// WithStore wires a caller-provided store implementation.
func WithStore(s store.Store) Option {
	return func(c *config) { c.store = s }
}

// WithAdminRegistrar wires the host application's admin shell.
func WithAdminRegistrar(r portslib.AdminRegistrar) Option {
	return func(c *config) { c.admin = r }
}

// WithHealthRegistrar wires the host application's health registrar.
func WithHealthRegistrar(r portslib.HealthRegistrar) Option {
	return func(c *config) { c.health = r }
}

// WithSQLiteDSN selects the default sqlite store using the caller-registered
// driver. The driver name defaults to "sqlite" (used by modernc.org/sqlite);
// override it via WithSQLiteDriver if the host registers under a different
// name.
func WithSQLiteDSN(dsn string) Option {
	return func(c *config) { c.sqliteDSN = dsn }
}

// WithSQLiteDriver overrides the sql.Open driver name used by WithSQLiteDSN.
// The default ("sqlite") matches modernc.org/sqlite, which is the OSS
// reference driver for v0.1.0.
func WithSQLiteDriver(driverName string) Option {
	return func(c *config) { c.sqliteDriver = driverName }
}
