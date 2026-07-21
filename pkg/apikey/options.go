package apikey

// Implements: REQ-APIKEY-001.
// Per: ADR-0017.
// Discipline: C-14.
// options.go owns functional options used by NewModule. New options should
// be purely additive; never change the meaning of an existing option.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

import (
	"github.com/septagon-oss/pk-core/pkg/security/passhash"

	"github.com/septagon-oss/pk-modules/pkg/apikey/store"
	"github.com/septagon-oss/pk-modules/pkg/audit"
	"github.com/septagon-oss/pk-modules/pkg/portslib"
)

// Option configures a Module at construction time.
type Option func(*config)

type config struct {
	store        store.Store
	hasher       passhash.Hasher
	audit        audit.AuditEmitter
	admin        portslib.AdminRegistrar
	health       portslib.HealthRegistrar
	sqliteDSN    string
	sqliteDriver string
}

// WithStore wires a caller-provided store implementation.
func WithStore(s store.Store) Option {
	return func(c *config) { c.store = s }
}

// WithHasher overrides the bcrypt hasher used for issuance and
// verification. The default is bcrypt at passhash.DefaultCost.
func WithHasher(h passhash.Hasher) Option {
	return func(c *config) { c.hasher = h }
}

// WithAuditEmitter wires an audit emitter used to record issue, verify,
// and revoke events. Optional.
func WithAuditEmitter(a audit.AuditEmitter) Option {
	return func(c *config) { c.audit = a }
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
// driver. The driver name defaults to "sqlite" (used by modernc.org/sqlite).
func WithSQLiteDSN(dsn string) Option {
	return func(c *config) { c.sqliteDSN = dsn }
}

// WithSQLiteDriver overrides the sql.Open driver name used by
// WithSQLiteDSN. The default ("sqlite") matches modernc.org/sqlite.
func WithSQLiteDriver(driverName string) Option {
	return func(c *config) { c.sqliteDriver = driverName }
}
