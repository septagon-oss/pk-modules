// Implements: REQ-BRANDING-001.
// Per: ADR-0017.
// Discipline: C-14.

package branding

// service.go owns the default Service: the business-rule layer between the
// branding_management HTTP surface (Task 5) and the persistence store. It
// validates and stamps records on Save/Skip and satisfies
// portslib.BrandingResolver so the admin shell and other modules can resolve
// a tenant's branding without depending on this package's concrete types.
//
// Color and font validation is delegated entirely to DeriveCSS in palette.go
// — this file never re-validates a hex color or a font key with its own
// regex. Logo validation (size + content sniffing) lives here because it is
// specific to the upload path, not to token derivation.
//
// ADR: ADR-0009 (ports-only module communication), ADR-0029 (file purpose declaration).
// Convention: C-10 (shared builders return errors), C-14 (every Go file declares its purpose).

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/septagon-oss/pk-modules/pkg/branding/store"
	"github.com/septagon-oss/pk-modules/pkg/portslib"
)

const (
	// maxLogoBytes bounds the accepted logo upload size (1 MiB).
	maxLogoBytes = 1 << 20
	// maxDisplayNameLen bounds the tenant display name.
	maxDisplayNameLen = 120
	// logoRoutePath is the servable logo route the HTTP handler (Task 5) will
	// mount. LogoURL is derived here so BrandingProfile never carries raw
	// bytes.
	logoRoutePath = "/api/v1/branding/logo"
)

// Compile-time proof Service satisfies the port the branding module provides
// (Effective Go "interface checks").
var _ portslib.BrandingResolver = (*Service)(nil)

// Service is the default portslib.BrandingResolver implementation. Pro can
// embed it to add object-storage-backed logos or richer font packs without
// reimplementing validation.
type Service struct {
	store store.Store
}

// NewService returns a Service backed by the given store.
func NewService(st store.Store) *Service {
	return &Service{store: st}
}

// SaveParams carries the fields a caller may set on a tenant's branding
// profile in one call. LogoData/LogoContentType/LogoAlt are optional as a
// unit: when LogoData is empty, Save preserves whatever logo (if any) is
// already on record rather than clearing it.
type SaveParams struct {
	DisplayName     string
	PrimaryColor    string
	FontKey         string
	LogoAlt         string
	LogoData        []byte
	LogoContentType string
}

// ResolveBranding returns the tenant's branding profile, or a zero profile
// (SetupComplete=false) and a nil error when no record exists yet.
func (s *Service) ResolveBranding(ctx context.Context, tenantID string) (portslib.BrandingProfile, error) {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return portslib.BrandingProfile{}, errors.New("branding: tenant id is required")
	}
	rec, err := s.store.Get(ctx, tenantID)
	if errors.Is(err, store.ErrNotFound) {
		return portslib.BrandingProfile{}, nil
	}
	if err != nil {
		return portslib.BrandingProfile{}, err
	}
	return toProfile(rec), nil
}

// BrandingCSS renders the tenant's derived theme overlay, delegating entirely
// to DeriveCSS. It returns "" when the tenant has no record, or a record with
// no color/font overrides.
func (s *Service) BrandingCSS(ctx context.Context, tenantID string) (string, error) {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return "", errors.New("branding: tenant id is required")
	}
	rec, err := s.store.Get(ctx, tenantID)
	if errors.Is(err, store.ErrNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return DeriveCSS(rec.PrimaryColor, rec.FontKey)
}

// Save validates and persists a tenant's branding choices, loading any
// existing record first so a Save without a new logo keeps the old one. It
// stamps SetupCompletedAt the first time a tenant successfully saves, and
// leaves it untouched on every later Save.
func (s *Service) Save(ctx context.Context, tenantID string, params SaveParams) error {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return errors.New("branding: tenant id is required")
	}

	rec, err := s.loadOrNew(ctx, tenantID)
	if err != nil {
		return err
	}

	displayName, err := validateDisplayName(params.DisplayName)
	if err != nil {
		return err
	}

	// Color/font validation is delegated to DeriveCSS — this is the single
	// gate for both fields; the derived CSS itself is discarded here.
	if _, err := DeriveCSS(params.PrimaryColor, params.FontKey); err != nil {
		return err
	}

	if len(params.LogoData) > 0 {
		if err := validateLogo(params.LogoData, params.LogoContentType); err != nil {
			return err
		}
		rec.LogoData = params.LogoData
		rec.LogoContentType = strings.TrimSpace(params.LogoContentType)
		rec.LogoAlt = strings.TrimSpace(params.LogoAlt)
	}
	// else: no new logo supplied, so rec keeps whatever logo it already had
	// (nothing, if this is the first Save).

	rec.DisplayName = displayName
	rec.PrimaryColor = strings.TrimSpace(params.PrimaryColor)
	rec.FontKey = strings.TrimSpace(params.FontKey)
	stampSetupComplete(rec)

	return s.store.Upsert(ctx, rec)
}

// Skip records that a tenant explicitly skipped branding setup, using
// fallbackName as the display name and leaving every other field empty —
// unlike Save, Skip does not preserve a prior record's logo or palette.
func (s *Service) Skip(ctx context.Context, tenantID, fallbackName string) error {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return errors.New("branding: tenant id is required")
	}
	displayName, err := validateDisplayName(fallbackName)
	if err != nil {
		return err
	}
	rec := &store.Record{TenantID: tenantID, DisplayName: displayName}
	stampSetupComplete(rec)
	return s.store.Upsert(ctx, rec)
}

// loadOrNew returns the tenant's existing record, or a fresh zero record for
// tenantID when none exists yet.
func (s *Service) loadOrNew(ctx context.Context, tenantID string) (*store.Record, error) {
	rec, err := s.store.Get(ctx, tenantID)
	if err == nil {
		return rec, nil
	}
	if errors.Is(err, store.ErrNotFound) {
		return &store.Record{TenantID: tenantID}, nil
	}
	return nil, err
}

// stampSetupComplete sets SetupCompletedAt to now if it is not already set,
// and leaves an existing stamp untouched.
func stampSetupComplete(rec *store.Record) {
	if rec.SetupCompletedAt != nil {
		return
	}
	now := time.Now().UTC()
	rec.SetupCompletedAt = &now
}

// validateDisplayName trims and bounds a display name.
func validateDisplayName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", errors.New("branding: display name is required")
	}
	if len(name) > maxDisplayNameLen {
		return "", fmt.Errorf("branding: display name exceeds %d characters", maxDisplayNameLen)
	}
	return name, nil
}

// validateLogo bounds the logo payload size and confirms its bytes actually
// match the declared content type. PNG/JPEG/WebP are verified with
// http.DetectContentType's magic-byte sniffing; SVG has no reliable magic
// bytes, so it is accepted only when declared image/svg+xml and its first 512
// bytes contain a "<svg" tag.
func validateLogo(data []byte, declared string) error {
	if len(data) > maxLogoBytes {
		return fmt.Errorf("branding: logo exceeds %d bytes", maxLogoBytes)
	}
	declared = strings.TrimSpace(declared)
	switch declared {
	case "image/svg+xml":
		probe := data
		if len(probe) > 512 {
			probe = probe[:512]
		}
		if !bytes.Contains(probe, []byte("<svg")) {
			return errors.New("branding: svg logo missing <svg element in first 512 bytes")
		}
		return nil
	case "image/png", "image/jpeg", "image/webp":
		if sniffed := http.DetectContentType(data); sniffed != declared {
			return fmt.Errorf("branding: logo content type %q does not match detected type %q", declared, sniffed)
		}
		return nil
	default:
		return fmt.Errorf("branding: unsupported logo content type %q", declared)
	}
}

// toProfile converts a stored record into the public BrandingProfile.
// LogoURL is a cache-busting servable route derived from UpdatedAt, never raw
// bytes, and is empty whenever the tenant has no logo on record.
func toProfile(rec *store.Record) portslib.BrandingProfile {
	profile := portslib.BrandingProfile{
		TenantID:      rec.TenantID,
		DisplayName:   rec.DisplayName,
		LogoAlt:       rec.LogoAlt,
		PrimaryColor:  rec.PrimaryColor,
		FontKey:       rec.FontKey,
		SetupComplete: rec.SetupCompletedAt != nil,
	}
	if len(rec.LogoData) > 0 {
		profile.LogoURL = logoRoutePath + "?v=" + strconv.FormatInt(rec.UpdatedAt.Unix(), 10)
	}
	return profile
}
