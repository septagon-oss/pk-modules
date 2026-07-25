// Validates: REQ-PORTS-001.
// Per: ADR-0009.
// Discipline: C-14.

package portslib_test

import (
	"testing"

	"github.com/septagon-oss/pk-modules/pkg/admin"
	"github.com/septagon-oss/pk-modules/pkg/apikey"
	"github.com/septagon-oss/pk-modules/pkg/audit"
	"github.com/septagon-oss/pk-modules/pkg/auth"
	"github.com/septagon-oss/pk-modules/pkg/content"
	"github.com/septagon-oss/pk-modules/pkg/health"
	"github.com/septagon-oss/pk-modules/pkg/notification"
	"github.com/septagon-oss/pk-modules/pkg/portslib"
	"github.com/septagon-oss/pk-modules/pkg/tenant"
	"github.com/septagon-oss/pk-modules/pkg/user"
)

func TestReleaseAndPortContractVersionsAreIndependent(t *testing.T) {
	t.Parallel()

	releases := map[string]string{
		admin.ModuleID:        admin.ReleaseVersion,
		apikey.ModuleID:       apikey.ReleaseVersion,
		audit.ModuleID:        audit.ReleaseVersion,
		auth.ModuleID:         auth.ReleaseVersion,
		content.ModuleID:      content.ReleaseVersion,
		health.ModuleID:       health.ReleaseVersion,
		notification.ModuleID: notification.ReleaseVersion,
		tenant.ModuleID:       tenant.ReleaseVersion,
		user.ModuleID:         user.ReleaseVersion,
	}
	// Every module publishes the single-sourced release line. The value
	// itself is asserted once, here, so a contract bump is a two-line diff:
	// portslib.ReleaseVersion and this expectation.
	if portslib.ReleaseVersion != "0.6.0" {
		t.Errorf("portslib.ReleaseVersion = %q, want 0.6.0", portslib.ReleaseVersion)
	}
	for moduleID, version := range releases {
		if version != portslib.ReleaseVersion {
			t.Errorf("%s release version = %q, want portslib.ReleaseVersion %q", moduleID, version, portslib.ReleaseVersion)
		}
	}

	unchangedContracts := map[string]string{
		apikey.ModuleID:       apikey.ModuleVersion,
		audit.ModuleID:        audit.ModuleVersion,
		auth.ModuleID:         auth.ModuleVersion,
		content.ModuleID:      content.ModuleVersion,
		health.ModuleID:       health.ModuleVersion,
		notification.ModuleID: notification.ModuleVersion,
		tenant.ModuleID:       tenant.ModuleVersion,
		user.ModuleID:         user.ModuleVersion,
	}
	for moduleID, version := range unchangedContracts {
		if version != "0.0.0" {
			t.Errorf("%s contract version = %q, want 0.0.0", moduleID, version)
		}
	}
	if admin.ModuleVersion != portslib.AdminRegistrarContractVersion {
		t.Fatalf(
			"admin contract version = %q, registrar contract = %q",
			admin.ModuleVersion,
			portslib.AdminRegistrarContractVersion,
		)
	}
}

func TestAdminComposePublishesReleaseAndContractVersions(t *testing.T) {
	t.Parallel()

	module, err := admin.NewModule()
	if err != nil {
		t.Fatalf("NewModule: %v", err)
	}
	composed := module.Compose()
	if got := composed.Metadata().Version; got != admin.ReleaseVersion {
		t.Fatalf("metadata version = %q, want release %q", got, admin.ReleaseVersion)
	}
	provides := composed.Provides()
	if len(provides) != 1 {
		t.Fatalf("admin provides %d ports, want 1", len(provides))
	}
	if got := provides[0].Version; got != portslib.AdminRegistrarContractVersion {
		t.Fatalf(
			"provided admin contract = %q, want %q",
			got,
			portslib.AdminRegistrarContractVersion,
		)
	}
}
