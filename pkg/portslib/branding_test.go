// Validates: REQ-PORTS-001.
// Per: ADR-0009.
// Discipline: C-14.

package portslib_test

// branding_test.go validates the tenant-branding port in portslib via the
// external test package, so the tests only depend on the public surface.
//
// ADR: ADR-0009 (ports-only module communication), ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

import (
	"context"
	"testing"

	"github.com/septagon-oss/pk-modules/pkg/portslib"
)

// stubBrandingResolver verifies that the BrandingResolver interface is
// satisfiable by a small custom implementation. The compile-check happens at
// the var _ portslib.BrandingResolver = stubBrandingResolver{} line below.
type stubBrandingResolver struct {
	profile portslib.BrandingProfile
	css     string
}

func (s stubBrandingResolver) ResolveBranding(_ context.Context, _ string) (portslib.BrandingProfile, error) {
	return s.profile, nil
}

func (s stubBrandingResolver) BrandingCSS(_ context.Context, _ string) (string, error) {
	return s.css, nil
}

var _ portslib.BrandingResolver = stubBrandingResolver{}

func TestStubBrandingResolverSatisfiesPort(t *testing.T) {
	t.Parallel()

	resolver := stubBrandingResolver{
		profile: portslib.BrandingProfile{
			TenantID:      "t-1",
			DisplayName:   "Acme",
			SetupComplete: true,
		},
		css: ":root { --brand-primary: #123456; }",
	}

	profile, err := resolver.ResolveBranding(context.Background(), "t-1")
	if err != nil {
		t.Fatalf("ResolveBranding: %v", err)
	}
	if profile.TenantID != "t-1" || profile.DisplayName != "Acme" || !profile.SetupComplete {
		t.Fatalf("ResolveBranding profile = %+v", profile)
	}

	css, err := resolver.BrandingCSS(context.Background(), "t-1")
	if err != nil {
		t.Fatalf("BrandingCSS: %v", err)
	}
	if css != ":root { --brand-primary: #123456; }" {
		t.Fatalf("BrandingCSS = %q", css)
	}
}

// TestBrandingProfileZeroValueMeansNotSetUp documents the port's contract:
// a zero BrandingProfile (returned for a tenant with no branding record)
// signals "not set up yet" so consumers fall back to their defaults and the
// admin shell shows the first-login setup screen.
func TestBrandingProfileZeroValueMeansNotSetUp(t *testing.T) {
	t.Parallel()

	var p portslib.BrandingProfile
	if p.SetupComplete {
		t.Fatalf("zero BrandingProfile.SetupComplete = true, want false")
	}
	if p.TenantID != "" || p.DisplayName != "" || p.LogoURL != "" || p.LogoAlt != "" ||
		p.PrimaryColor != "" || p.FontKey != "" {
		t.Fatalf("zero BrandingProfile has unexpected non-empty fields: %+v", p)
	}
}
