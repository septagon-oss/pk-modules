package auth

// options.go owns functional options used by NewModule. New options should
// be purely additive; never change the meaning of an existing option.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

import (
	"database/sql"
	"time"

	"github.com/septagon-oss/pk-core/pkg/security/passhash"

	"github.com/septagon-oss/pk-modules/pkg/audit"
	"github.com/septagon-oss/pk-modules/pkg/portslib"
	"github.com/septagon-oss/pk-modules/pkg/user"
)

// Option configures a Module at construction time.
type Option func(*config)

type config struct {
	sessions     SessionStore
	users        user.UserBoundaryReader
	hasher       passhash.Hasher
	policy       LoginPolicy
	audit        audit.AuditEmitter
	admin        portslib.AdminRegistrar
	health       portslib.HealthRegistrar
	sessionTTL   time.Duration
	sqliteDB     *sql.DB
	sqliteDSN    string
	sqliteDriver string
}

// WithSessionStore wires a caller-provided SessionStore implementation.
func WithSessionStore(s SessionStore) Option {
	return func(c *config) { c.sessions = s }
}

// WithUserReader wires the user_management read-port used to resolve
// credentials. Auth needs at least the PassHash and Active fields on the
// returned user.User.
func WithUserReader(r user.UserBoundaryReader) Option {
	return func(c *config) { c.users = r }
}

// WithHasher selects the password hasher used to verify the stored
// PassHash. The default mirrors user_management: bcrypt at
// passhash.DefaultCost.
func WithHasher(h passhash.Hasher) Option {
	return func(c *config) { c.hasher = h }
}

// WithLoginPolicy installs the policy hook consulted on every login. The
// default PermissiveLoginPolicy never blocks.
func WithLoginPolicy(p LoginPolicy) Option {
	return func(c *config) { c.policy = p }
}

// WithSessionTTL overrides the default session lifetime. The OSS default
// is 24h; production deployments typically lower this.
func WithSessionTTL(d time.Duration) Option {
	return func(c *config) { c.sessionTTL = d }
}

// WithAuditEmitter wires an audit emitter used to record login successes
// and failures. Optional; absent emitter silently disables auditing.
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

// WithSQLiteDB wires the default sqlite session store on top of a caller-owned
// *sql.DB. Use this when several modules must share one connection pool over a
// single SQLite file — the host opens one *sql.DB (typically with
// SetMaxOpenConns(1)) and hands the same handle to every module so they cannot
// race each other's schema creation or fan out into independent pools. The
// caller retains ownership of the *sql.DB lifecycle (Close). It wins over
// WithSQLiteDSN but loses to an explicit WithSessionStore.
func WithSQLiteDB(db *sql.DB) Option {
	return func(c *config) { c.sqliteDB = db }
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
