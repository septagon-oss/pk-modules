// Validates: REQ-BRANDING-001.
// Per: ADR-0017.
// Discipline: C-14.

package branding_test

// service_test.go validates the default Service against its public API:
// ResolveBranding/BrandingCSS satisfy the portslib.BrandingResolver contract
// on an empty store, Save's validation table (display name — including
// multibyte rune-counted boundaries — delegated color/font, and logo size +
// content sniffing), the SetupCompletedAt stamping rule, Skip's
// fallback-name-only write, Logo's uniform-404 sentinel, the derived LogoURL
// and BrandingCSS outputs, and the blank-tenant-ID guard shared by every
// public method. Tests live in branding_test to exercise the package the way
// callers see it.
//
// ADR: ADR-0009 (ports-only module communication), ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

import (
	"context"
	"errors"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/septagon-oss/pk-modules/pkg/branding"
	"github.com/septagon-oss/pk-modules/pkg/branding/store"
	"github.com/septagon-oss/pk-modules/pkg/branding/store/sqlite"
	"github.com/septagon-oss/pk-modules/pkg/portslib"

	_ "modernc.org/sqlite"
)

// pngBytes, jpegBytes, and webpBytes are minimal payloads carrying just the
// magic bytes http.DetectContentType keys off, padded so they clear typical
// "too small to sniff" edge cases.
var (
	pngBytes  = append([]byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}, []byte("restofpngdata")...)
	jpegBytes = append([]byte{0xFF, 0xD8, 0xFF, 0xE0}, []byte("restofjpegdata")...)
	webpBytes = append([]byte("RIFF\x00\x00\x00\x00WEBPVP"), []byte("8 restofwebpdata")...)
)

// maxLogoBytesForTest mirrors the unexported maxLogoBytes bound in
// service.go (1 MiB). It cannot be imported here — these tests live in the
// external branding_test package — so it is pinned once, here, as the single
// source both service_test.go's and handler_test.go's oversized/exactly-max
// logo fixtures build from; a change to the real constant is easy to notice
// and follow from this one spot.
const maxLogoBytesForTest = 1 << 20

// exactlyMaxSizeLogoBytes and oversizedLogoBytes are PNG-prefixed fixtures at
// the maxLogoBytesForTest boundary — the former exactly at the limit (must be
// accepted), the latter one byte past it (must be rejected on size, not on
// content sniffing, hence the real PNG signature prefix on both).
var (
	exactlyMaxSizeLogoBytes = padLogoBytes(pngBytes, maxLogoBytesForTest)
	oversizedLogoBytes      = padLogoBytes(pngBytes, maxLogoBytesForTest+1)
)

// padLogoBytes returns data extended with zero bytes to exactly size, so the
// result still starts with data's real magic-byte signature.
func padLogoBytes(data []byte, size int) []byte {
	out := make([]byte, size)
	copy(out, data)
	return out
}

const validSVG = `<?xml version="1.0" encoding="UTF-8"?><svg xmlns="http://www.w3.org/2000/svg"><rect/></svg>`

// newStore returns a fresh sqlite-backed store.Store on an isolated on-disk
// temp file, mirroring store/sqlite's own test helper.
func newStore(t *testing.T) store.Store {
	t.Helper()
	dsn := "file:" + filepath.Join(t.TempDir(), "branding.db") + "?_pragma=journal_mode(WAL)"
	s, err := sqlite.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	t.Cleanup(func() { _ = s.DB().Close() })
	return s
}

// newTestService returns a Service and its backing store so tests can both
// exercise the public API and inspect persisted rows directly.
func newTestService(t *testing.T) (*branding.Service, store.Store) {
	t.Helper()
	st := newStore(t)
	return branding.NewService(st), st
}

func TestResolveBrandingEmptyStoreReturnsZeroProfile(t *testing.T) {
	t.Parallel()
	svc, _ := newTestService(t)

	profile, err := svc.ResolveBranding(context.Background(), "tenant-1")
	if err != nil {
		t.Fatalf("ResolveBranding: %v", err)
	}
	if profile != (portslib.BrandingProfile{}) {
		t.Fatalf("profile = %+v, want zero value", profile)
	}
	if profile.SetupComplete {
		t.Fatalf("SetupComplete = true on empty store, want false")
	}
}

// TestEmptyTenantIDRejectedByEveryPublicMethod pins that every public method
// on Service shares one guard: a blank (or whitespace-only) tenant ID always
// errors, and — because the guard runs first — never reaches the store.
func TestEmptyTenantIDRejectedByEveryPublicMethod(t *testing.T) {
	t.Parallel()
	svc, _ := newTestService(t)
	ctx := context.Background()

	cases := []struct {
		name string
		call func() error
	}{
		{"ResolveBranding", func() error {
			_, err := svc.ResolveBranding(ctx, "   ")
			return err
		}},
		{"BrandingCSS", func() error {
			_, err := svc.BrandingCSS(ctx, "")
			return err
		}},
		{"Save", func() error {
			return svc.Save(ctx, "", branding.SaveParams{DisplayName: "Acme Ops"})
		}},
		{"Skip", func() error {
			return svc.Skip(ctx, "  ", "Acme Ops")
		}},
		{"Logo", func() error {
			_, _, err := svc.Logo(ctx, "")
			return err
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if err := tc.call(); err == nil {
				t.Fatalf("%s with a blank tenant ID should error", tc.name)
			}
		})
	}
}

func TestSaveDisplayNameValidation(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		displayName string
		wantErr     bool
	}{
		{"ok", "Acme Ops", false},
		{"trims to empty", "   ", true},
		{"empty", "", true},
		{"exactly max length ok (ascii)", strings.Repeat("a", 120), false},
		{"over max length rejected (ascii)", strings.Repeat("a", 121), true},
		// The bound is counted in runes, not bytes: "名" is a 3-byte UTF-8
		// rune, so 120 of them is ~360 bytes but exactly 120 characters — a
		// byte-counted check would wrongly reject this.
		{"exactly max length ok (multibyte, 120 runes)", strings.Repeat("名", 120), false},
		{"over max length rejected (multibyte, 121 runes)", strings.Repeat("名", 121), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// Each subtest gets its own store: sharing one store.Store across
			// parallel writers (no busy-timeout pragma configured) trips
			// SQLITE_BUSY rather than serializing the writes.
			svc, _ := newTestService(t)
			err := svc.Save(context.Background(), "tenant", branding.SaveParams{DisplayName: tc.displayName})
			if (err != nil) != tc.wantErr {
				t.Fatalf("Save err = %v, wantErr = %v", err, tc.wantErr)
			}
		})
	}
}

// TestSavePaletteValidationDelegatesToPalette proves Save routes color/font
// validation through DeriveCSS rather than re-validating with its own rules.
// It deliberately does not duplicate palette_test.go's exhaustive cases —
// just enough to prove one bad value of each kind surfaces as an error.
func TestSavePaletteValidationDelegatesToPalette(t *testing.T) {
	t.Parallel()

	t.Run("invalid hex color", func(t *testing.T) {
		t.Parallel()
		svc, _ := newTestService(t)
		err := svc.Save(context.Background(), "tenant", branding.SaveParams{
			DisplayName:  "Acme Ops",
			PrimaryColor: "not-a-color",
		})
		if err == nil {
			t.Fatalf("Save with invalid color should error")
		}
	})

	t.Run("unknown font key", func(t *testing.T) {
		t.Parallel()
		svc, _ := newTestService(t)
		err := svc.Save(context.Background(), "tenant", branding.SaveParams{
			DisplayName: "Acme Ops",
			FontKey:     "definitely-not-curated",
		})
		if err == nil {
			t.Fatalf("Save with unknown font key should error")
		}
	})

	t.Run("valid color and font accepted", func(t *testing.T) {
		t.Parallel()
		svc, _ := newTestService(t)
		err := svc.Save(context.Background(), "tenant", branding.SaveParams{
			DisplayName:  "Acme Ops",
			PrimaryColor: "#14b8a6",
			FontKey:      "editorial",
		})
		if err != nil {
			t.Fatalf("Save with valid palette: %v", err)
		}
	})
}

// TestSaveLogoAltValidation proves logo alt text is bounded to the same
// rune-counted budget as the display name (validateLogoAlt reuses
// maxDisplayNameLen) but, unlike the display name, is optional: an empty
// value is valid.
func TestSaveLogoAltValidation(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		alt     string
		wantErr bool
	}{
		{"ok", "Acme logo", false},
		{"empty ok (alt text is optional)", "", false},
		{"trims to empty ok", "   ", false},
		{"exactly max length ok (ascii)", strings.Repeat("a", 120), false},
		{"over max length rejected (ascii)", strings.Repeat("a", 121), true},
		{"exactly max length ok (multibyte, 120 runes)", strings.Repeat("名", 120), false},
		{"over max length rejected (multibyte, 121 runes)", strings.Repeat("名", 121), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			svc, _ := newTestService(t)
			err := svc.Save(context.Background(), "tenant", branding.SaveParams{
				DisplayName:     "Acme Ops",
				LogoData:        pngBytes,
				LogoContentType: "image/png",
				LogoAlt:         tc.alt,
			})
			if (err != nil) != tc.wantErr {
				t.Fatalf("Save err = %v, wantErr = %v", err, tc.wantErr)
			}
		})
	}
}

func TestSaveLogoValidation(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		data        []byte
		contentType string
		wantErr     bool
	}{
		{"png accepted", pngBytes, "image/png", false},
		{"jpeg accepted", jpegBytes, "image/jpeg", false},
		{"webp accepted", webpBytes, "image/webp", false},
		{"svg accepted with svg tag", []byte(validSVG), "image/svg+xml", false},
		{"svg rejected without svg tag", []byte(`<?xml version="1.0"?><notsvg/>`), "image/svg+xml", true},
		{"png declared as jpeg rejected", pngBytes, "image/jpeg", true},
		{"random bytes rejected", []byte("this is not an image at all, just text"), "image/png", true},
		{"unsupported declared type rejected", pngBytes, "image/gif", true},
		{"exactly max size accepted", exactlyMaxSizeLogoBytes, "image/png", false},
		{"oversized logo rejected", oversizedLogoBytes, "image/png", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			svc, _ := newTestService(t)
			err := svc.Save(context.Background(), "tenant", branding.SaveParams{
				DisplayName:     "Acme Ops",
				LogoData:        tc.data,
				LogoContentType: tc.contentType,
			})
			if (err != nil) != tc.wantErr {
				t.Fatalf("Save err = %v, wantErr = %v", err, tc.wantErr)
			}
		})
	}
}

func TestSaveStampsSetupCompletedOnFirstSaveOnly(t *testing.T) {
	t.Parallel()
	svc, st := newTestService(t)
	ctx := context.Background()
	tenantID := "tenant-stamp"

	if err := svc.Save(ctx, tenantID, branding.SaveParams{DisplayName: "Acme Ops"}); err != nil {
		t.Fatalf("first Save: %v", err)
	}
	profile, err := svc.ResolveBranding(ctx, tenantID)
	if err != nil {
		t.Fatalf("ResolveBranding after first Save: %v", err)
	}
	if !profile.SetupComplete {
		t.Fatalf("SetupComplete = false after first Save, want true")
	}
	first, err := st.Get(ctx, tenantID)
	if err != nil {
		t.Fatalf("Get after first Save: %v", err)
	}
	if first.SetupCompletedAt == nil {
		t.Fatalf("SetupCompletedAt is nil after first Save")
	}

	time.Sleep(2 * time.Millisecond)

	if err := svc.Save(ctx, tenantID, branding.SaveParams{DisplayName: "Acme Ops Renamed"}); err != nil {
		t.Fatalf("second Save: %v", err)
	}
	profile, err = svc.ResolveBranding(ctx, tenantID)
	if err != nil {
		t.Fatalf("ResolveBranding after second Save: %v", err)
	}
	if !profile.SetupComplete {
		t.Fatalf("SetupComplete = false after second Save, want true")
	}
	second, err := st.Get(ctx, tenantID)
	if err != nil {
		t.Fatalf("Get after second Save: %v", err)
	}
	if second.SetupCompletedAt == nil {
		t.Fatalf("SetupCompletedAt is nil after second Save")
	}
	if !second.SetupCompletedAt.Equal(*first.SetupCompletedAt) {
		t.Fatalf("SetupCompletedAt changed on second Save: first=%v second=%v",
			first.SetupCompletedAt, second.SetupCompletedAt)
	}
}

func TestSaveWithoutNewLogoPreservesExistingLogo(t *testing.T) {
	t.Parallel()
	svc, st := newTestService(t)
	ctx := context.Background()
	tenantID := "tenant-preserve-logo"

	if err := svc.Save(ctx, tenantID, branding.SaveParams{
		DisplayName:     "Acme Ops",
		LogoData:        pngBytes,
		LogoContentType: "image/png",
		LogoAlt:         "Acme logo",
	}); err != nil {
		t.Fatalf("Save with logo: %v", err)
	}

	if err := svc.Save(ctx, tenantID, branding.SaveParams{DisplayName: "Acme Ops Renamed"}); err != nil {
		t.Fatalf("Save without logo: %v", err)
	}

	got, err := st.Get(ctx, tenantID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(got.LogoData) != string(pngBytes) || got.LogoContentType != "image/png" || got.LogoAlt != "Acme logo" {
		t.Fatalf("logo not preserved across Save without new logo: %+v", got)
	}
	if got.DisplayName != "Acme Ops Renamed" {
		t.Fatalf("DisplayName = %q, want %q", got.DisplayName, "Acme Ops Renamed")
	}
}

func TestSkipWritesFallbackNameOnly(t *testing.T) {
	t.Parallel()
	svc, _ := newTestService(t)
	ctx := context.Background()
	tenantID := "tenant-skip"

	// Seed a fuller record first to prove Skip does not preserve it — unlike
	// Save, "everything else empty" applies even when a prior record exists.
	if err := svc.Save(ctx, tenantID, branding.SaveParams{
		DisplayName:     "Acme Ops",
		PrimaryColor:    "#14b8a6",
		FontKey:         "editorial",
		LogoData:        pngBytes,
		LogoContentType: "image/png",
		LogoAlt:         "Acme logo",
	}); err != nil {
		t.Fatalf("seed Save: %v", err)
	}

	if err := svc.Skip(ctx, tenantID, "Acme Ops Fallback"); err != nil {
		t.Fatalf("Skip: %v", err)
	}

	profile, err := svc.ResolveBranding(ctx, tenantID)
	if err != nil {
		t.Fatalf("ResolveBranding: %v", err)
	}
	if !profile.SetupComplete {
		t.Fatalf("SetupComplete = false after Skip, want true")
	}
	if profile.DisplayName != "Acme Ops Fallback" {
		t.Fatalf("DisplayName = %q, want %q", profile.DisplayName, "Acme Ops Fallback")
	}
	if profile.PrimaryColor != "" || profile.FontKey != "" || profile.LogoURL != "" || profile.LogoAlt != "" {
		t.Fatalf("Skip should clear every other field: %+v", profile)
	}
}

func TestSkipRequiresFallbackName(t *testing.T) {
	t.Parallel()
	svc, _ := newTestService(t)
	if err := svc.Skip(context.Background(), "tenant-skip-empty", "   "); err == nil {
		t.Fatalf("Skip with blank fallback name should error")
	}
}

// TestLogo covers Service.Logo's three outcomes: no record at all, a record
// with no logo, and a record with one. The first two must both surface
// store.ErrNotFound — that is the whole point of the seam, so the HTTP
// handler (Task 5) can 404 uniformly without distinguishing them.
func TestLogo(t *testing.T) {
	t.Parallel()

	t.Run("no record", func(t *testing.T) {
		t.Parallel()
		svc, _ := newTestService(t)
		if _, _, err := svc.Logo(context.Background(), "tenant"); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("Logo err = %v, want ErrNotFound", err)
		}
	})

	t.Run("record without logo", func(t *testing.T) {
		t.Parallel()
		svc, _ := newTestService(t)
		ctx := context.Background()
		if err := svc.Save(ctx, "tenant", branding.SaveParams{DisplayName: "Acme Ops"}); err != nil {
			t.Fatalf("Save: %v", err)
		}
		if _, _, err := svc.Logo(ctx, "tenant"); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("Logo err = %v, want ErrNotFound", err)
		}
	})

	t.Run("record with logo", func(t *testing.T) {
		t.Parallel()
		svc, _ := newTestService(t)
		ctx := context.Background()
		if err := svc.Save(ctx, "tenant", branding.SaveParams{
			DisplayName:     "Acme Ops",
			LogoData:        pngBytes,
			LogoContentType: "image/png",
		}); err != nil {
			t.Fatalf("Save: %v", err)
		}
		data, contentType, err := svc.Logo(ctx, "tenant")
		if err != nil {
			t.Fatalf("Logo: %v", err)
		}
		if string(data) != string(pngBytes) {
			t.Fatalf("Logo data = %v, want %v", data, pngBytes)
		}
		if contentType != "image/png" {
			t.Fatalf("Logo content type = %q, want %q", contentType, "image/png")
		}
	})
}

func TestResolveBrandingLogoURL(t *testing.T) {
	t.Parallel()

	t.Run("with logo", func(t *testing.T) {
		t.Parallel()
		svc, st := newTestService(t)
		ctx := context.Background()
		tenantID := "tenant"
		if err := svc.Save(ctx, tenantID, branding.SaveParams{
			DisplayName:     "Acme Ops",
			LogoData:        pngBytes,
			LogoContentType: "image/png",
		}); err != nil {
			t.Fatalf("Save: %v", err)
		}
		rec, err := st.Get(ctx, tenantID)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		profile, err := svc.ResolveBranding(ctx, tenantID)
		if err != nil {
			t.Fatalf("ResolveBranding: %v", err)
		}
		want := "/api/v1/branding/logo?v=" + strconv.FormatInt(rec.UpdatedAt.Unix(), 10)
		if profile.LogoURL != want {
			t.Fatalf("LogoURL = %q, want %q", profile.LogoURL, want)
		}
	})

	t.Run("without logo", func(t *testing.T) {
		t.Parallel()
		svc, _ := newTestService(t)
		ctx := context.Background()
		tenantID := "tenant"
		if err := svc.Save(ctx, tenantID, branding.SaveParams{DisplayName: "Acme Ops"}); err != nil {
			t.Fatalf("Save: %v", err)
		}
		profile, err := svc.ResolveBranding(ctx, tenantID)
		if err != nil {
			t.Fatalf("ResolveBranding: %v", err)
		}
		if profile.LogoURL != "" {
			t.Fatalf("LogoURL = %q, want empty", profile.LogoURL)
		}
	})
}

func TestBrandingCSS(t *testing.T) {
	t.Parallel()

	t.Run("no record", func(t *testing.T) {
		t.Parallel()
		svc, _ := newTestService(t)
		css, err := svc.BrandingCSS(context.Background(), "tenant")
		if err != nil {
			t.Fatalf("BrandingCSS: %v", err)
		}
		if css != "" {
			t.Fatalf("BrandingCSS = %q, want empty", css)
		}
	})

	t.Run("record with no overrides", func(t *testing.T) {
		t.Parallel()
		svc, _ := newTestService(t)
		ctx := context.Background()
		tenantID := "tenant"
		if err := svc.Save(ctx, tenantID, branding.SaveParams{DisplayName: "Acme Ops"}); err != nil {
			t.Fatalf("Save: %v", err)
		}
		css, err := svc.BrandingCSS(ctx, tenantID)
		if err != nil {
			t.Fatalf("BrandingCSS: %v", err)
		}
		if css != "" {
			t.Fatalf("BrandingCSS = %q, want empty", css)
		}
	})

	t.Run("record with overrides matches DeriveCSS", func(t *testing.T) {
		t.Parallel()
		svc, _ := newTestService(t)
		ctx := context.Background()
		tenantID := "tenant"
		if err := svc.Save(ctx, tenantID, branding.SaveParams{
			DisplayName:  "Acme Ops",
			PrimaryColor: "#14b8a6",
			FontKey:      "grotesk",
		}); err != nil {
			t.Fatalf("Save: %v", err)
		}
		css, err := svc.BrandingCSS(ctx, tenantID)
		if err != nil {
			t.Fatalf("BrandingCSS: %v", err)
		}
		want, err := branding.DeriveCSS("#14b8a6", "grotesk")
		if err != nil {
			t.Fatalf("DeriveCSS: %v", err)
		}
		if css != want {
			t.Fatalf("BrandingCSS = %q, want %q", css, want)
		}
	})
}
