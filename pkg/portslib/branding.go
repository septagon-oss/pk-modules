// Implements: REQ-PORTS-001.
// Per: ADR-0009.
// Discipline: C-14.

package portslib

// branding.go owns the tenant-branding port. The branding module provides it;
// the admin shell consumes it to theme the chrome per tenant and to gate the
// first-login setup. Pro provides richer implementations (object-storage
// logos, font packs) behind this same interface.
//
// ADR: ADR-0009 (ports-only module communication), ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

import "context"

// BrandingContractVersion is the compatibility version of BrandingResolver.
const BrandingContractVersion = "0.1.0"

// BrandingProfile is one tenant's resolved branding. A zero profile (absent
// record) means "not set up yet": consumers fall back to their defaults and
// the admin shell shows the first-login setup. The shell gates any
// authenticated session tenant whose profile has SetupComplete false —
// including the zero/absent-record profile, whose empty TenantID must not be
// read as "no gate". LogoURL is a servable route, never raw bytes, so
// providers can back it with any storage.
type BrandingProfile struct {
	TenantID      string
	DisplayName   string
	LogoURL       string
	LogoAlt       string
	PrimaryColor  string // #rrggbb, empty = default palette
	FontKey       string // curated key, empty = default stacks
	SetupComplete bool
}

// BrandingResolver resolves tenant branding and its derived theme CSS.
// ResolveBranding returns a zero profile and nil error when no record exists.
// BrandingCSS returns a ready-to-serve CSS document (custom-property
// overrides only; empty string when the tenant has no overrides).
type BrandingResolver interface {
	ResolveBranding(ctx context.Context, tenantID string) (BrandingProfile, error)
	BrandingCSS(ctx context.Context, tenantID string) (string, error)
}
