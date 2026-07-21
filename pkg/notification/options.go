package notification

// Implements: REQ-NOTIF-002.
// Per: ADR-0017.
// Discipline: C-14.
// options.go owns functional options used by NewModule. New options should be
// purely additive; never change the meaning of an existing option.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

import (
	"github.com/septagon-oss/pk-core/pkg/event"

	"github.com/septagon-oss/pk-modules/pkg/audit"
	"github.com/septagon-oss/pk-modules/pkg/notification/store"
	"github.com/septagon-oss/pk-modules/pkg/portslib"
	"github.com/septagon-oss/pk-modules/pkg/user"
)

// Option configures a Module at construction time.
type Option func(*config)

type config struct {
	store        store.Store
	channels     []portslib.NotificationChannel
	bus          event.Bus
	users        user.UserBoundaryReader
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

// WithChannel registers an additional NotificationChannel. WithChannel can be
// called multiple times; channels are dispatched in registration order.
func WithChannel(ch portslib.NotificationChannel) Option {
	return func(c *config) {
		if ch == nil {
			return
		}
		c.channels = append(c.channels, ch)
	}
}

// WithEventBus wires an event bus used for cross-module notification emit
// fan-out (Pro uses this to forward to external transports). Optional.
func WithEventBus(b event.Bus) Option {
	return func(c *config) { c.bus = b }
}

// WithUserReader wires the user_management read port used to validate
// recipient identity at dispatch time. Optional.
func WithUserReader(u user.UserBoundaryReader) Option {
	return func(c *config) { c.users = u }
}

// WithAuditEmitter wires an audit emitter used to record dispatch events.
// Optional; absent emitter silently disables auditing.
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

// WithSQLiteDriver overrides the sql.Open driver name used by WithSQLiteDSN.
// The default ("sqlite") matches modernc.org/sqlite.
func WithSQLiteDriver(driverName string) Option {
	return func(c *config) { c.sqliteDriver = driverName }
}
