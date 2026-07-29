package admin

// Implements: REQ-ADMIN-001.
// Per: ADR-0032.
// Discipline: C-14.
// constants.go owns the package-level defaults used by NewShell and NewModule
// when callers leave a field zero.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

const (
	defaultTitle    = "PlatformKit Admin"
	defaultBasePath = "/admin"

	// brandingPathSuffix is the path (under basePath) where the branding
	// module registers its admin page — branding's adminPagePathSuffix. The
	// shell's first-login gate both exempts and redirects to
	// basePath+brandingPathSuffix, so the two declarations must agree.
	brandingPathSuffix = "/branding"
)
