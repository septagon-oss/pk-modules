package notification

// module.go owns the singleton wiring for notification_management: NewModule
// constructs an OSS *Module, Compose() returns the module.Composable used by
// the catalog, and Service exposes the public port.
//
// Pro embeds *Module to add mail/SMS/push channels, digesting, throttling,
// and templated rendering without changing the OSS surface.
//
// ADR: ADR-0009 (ports-only module communication), ADR-0017 (composition through dependency injection), ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

import (
	"context"
	"errors"
	"fmt"
	"io/fs"

	"github.com/septagon-oss/pk-core/pkg/event"
	pkmodule "github.com/septagon-oss/pk-core/pkg/module"
	"github.com/septagon-oss/pk-core/pkg/observability/health"

	"github.com/septagon-oss/pk-modules/pkg/audit"
	"github.com/septagon-oss/pk-modules/pkg/notification/migrations"
	"github.com/septagon-oss/pk-modules/pkg/notification/store"
	"github.com/septagon-oss/pk-modules/pkg/notification/store/sqlite"
	"github.com/septagon-oss/pk-modules/pkg/portslib"
	"github.com/septagon-oss/pk-modules/pkg/user"
)

// Module metadata constants used by both the catalog and admin shell.
const (
	ModuleID          = "notification_management"
	ModuleName        = "Notification Management"
	ModuleDescription = "In-app notifications with pluggable channels and per-user subscriptions."
	ModuleVersion     = "0.0.0"
)

// defaultSQLiteDriver matches modernc.org/sqlite's default registration name.
const defaultSQLiteDriver = "sqlite"

// Module is the OSS notification_management module. Pro embeds *Module and
// adds Pro-only fields/methods.
type Module struct {
	metadata pkmodule.Metadata
	store    store.Store
	channels []portslib.NotificationChannel
	bus      event.Bus
	users    user.UserBoundaryReader
	audit    audit.AuditEmitter
	admin    portslib.AdminRegistrar
	health   portslib.HealthRegistrar
	svc      *service
	handler  *Handler
}

// NewModule constructs a notification module.
func NewModule(opts ...Option) (*Module, error) {
	cfg := config{
		sqliteDriver: defaultSQLiteDriver,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	if cfg.admin == nil {
		cfg.admin = portslib.NoopAdminRegistrar()
	}
	if cfg.health == nil {
		cfg.health = portslib.NoopHealthRegistrar()
	}

	st, err := resolveStore(cfg)
	if err != nil {
		return nil, err
	}

	// Always prepend the built-in in-app channel so a default install
	// persists notifications without further wiring. Callers can register
	// additional channels via WithChannel; the in-app channel always runs
	// first.
	channels := make([]portslib.NotificationChannel, 0, 1+len(cfg.channels))
	channels = append(channels, &inAppChannel{store: st})
	channels = append(channels, cfg.channels...)

	m := &Module{
		metadata: pkmodule.Metadata{
			ID:          ModuleID,
			Name:        ModuleName,
			Description: ModuleDescription,
			Version:     ModuleVersion,
		},
		store:    st,
		channels: channels,
		bus:      cfg.bus,
		users:    cfg.users,
		audit:    cfg.audit,
		admin:    cfg.admin,
		health:   cfg.health,
	}
	m.svc = newService(st, channels, cfg.audit)
	m.handler = NewHandler(m.svc)

	if err := registerAdmin(cfg.admin); err != nil {
		return nil, err
	}
	if err := registerHealth(cfg.health, st); err != nil {
		return nil, err
	}
	return m, nil
}

// MustNewModule is the panic-on-error variant of NewModule.
func MustNewModule(opts ...Option) *Module {
	m, err := NewModule(opts...)
	if err != nil {
		panic(err)
	}
	return m
}

func resolveStore(cfg config) (store.Store, error) {
	switch {
	case cfg.store != nil:
		return cfg.store, nil
	case cfg.sqliteDSN != "":
		st, err := sqlite.Open(cfg.sqliteDriver, cfg.sqliteDSN)
		if err != nil {
			return nil, fmt.Errorf("notification: open sqlite store: %w", err)
		}
		return st, nil
	default:
		return nil, errors.New("notification: no store configured — use WithStore or WithSQLiteDSN")
	}
}

// registerHealth wires a lightweight check that the store responds to a
// bounded GetByUser call.
func registerHealth(r portslib.HealthRegistrar, st store.Store) error {
	if r == nil {
		return nil
	}
	checker := health.CheckerFunc(func(ctx context.Context) error {
		_, err := st.GetByUser(ctx, "_health_probe_", 1, 0)
		return err
	})
	return r.Register("notification_management.store", checker)
}

// Compose returns the module.Composable representation the catalog consumes
// when validating port wiring.
func (m *Module) Compose() pkmodule.Composable {
	return pkmodule.Must(m.metadata,
		pkmodule.WithProvides(
			pkmodule.Provide[NotificationService](ModuleVersion),
		),
		pkmodule.WithDependencies(
			pkmodule.Optional[user.UserBoundaryReader](pkmodule.DependencySpec{
				Version:           "0.0.0",
				Purpose:           "Validate recipient identity at dispatch time.",
				Category:          pkmodule.DependencyCategoryBusiness,
				SubCategory:       "user",
				PreferredProvider: "user_management",
			}),
			pkmodule.Optional[audit.AuditEmitter](pkmodule.DependencySpec{
				Version:           "0.0.0",
				Purpose:           "Emit notification.dispatched events.",
				Category:          pkmodule.DependencyCategorySecurity,
				SubCategory:       "audit",
				PreferredProvider: "audit_management",
			}),
			pkmodule.Optional[portslib.AdminRegistrar](pkmodule.DependencySpec{
				Version:           "0.0.0",
				Purpose:           "Mount the notifications admin page.",
				Category:          pkmodule.DependencyCategoryUI,
				SubCategory:       "admin",
				PreferredProvider: "admin_management",
			}),
			pkmodule.Optional[portslib.HealthRegistrar](pkmodule.DependencySpec{
				Version:           "0.0.0",
				Purpose:           "Surface notification_management store reachability.",
				Category:          pkmodule.DependencyCategoryMonitoring,
				SubCategory:       "health",
				PreferredProvider: "health_management",
			}),
		),
	)
}

// Migrations exposes the embedded migrations FS for app-level migration runners.
func (m *Module) Migrations() fs.FS { return migrations.FS() }

// Service returns the NotificationService backed by this module's store.
func (m *Module) Service() NotificationService { return m.svc }

// HTTPHandler returns the wired HTTP handler so the host application can
// mount it on its router of choice.
func (m *Module) HTTPHandler() *Handler { return m.handler }

// Channels returns the registered channel chain (the in-app channel first,
// then any caller-supplied channels) so Pro can introspect and reorder.
func (m *Module) Channels() []portslib.NotificationChannel {
	out := make([]portslib.NotificationChannel, len(m.channels))
	copy(out, m.channels)
	return out
}

// Store returns the underlying Store so Pro can wrap with auditing.
func (m *Module) Store() store.Store { return m.store }
