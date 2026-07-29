// Implements: REQ-ADMIN-001.
// Per: ADR-0017.
// Discipline: C-14.

package admin

// options.go owns functional options used by NewModule. New options must be
// purely additive; never change the meaning of an existing option.
//
// ADR: ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

import "github.com/septagon-oss/pk-modules/pkg/portslib"

// Option configures a Module at construction time.
type Option func(*config)

type config struct {
	title    string
	basePath string
	branding portslib.BrandingResolver
}

// WithTitle overrides the admin shell title rendered in the layout and the
// HTML <title> tag. Defaults to "PlatformKit Admin".
func WithTitle(title string) Option {
	return func(c *config) { c.title = title }
}

// WithBasePath overrides the URL prefix the admin shell is mounted under.
// Defaults to "/admin". The handler also rewrites static asset URLs to use
// this prefix.
func WithBasePath(path string) Option {
	return func(c *config) { c.basePath = path }
}

// WithBranding wires a tenant branding resolver into the shell. When set, the
// chrome themes itself per tenant (display name, logo, favicon, theme CSS at
// {basePath}/static/_branding.css) and incomplete setup redirects to the
// branding page. Nil (the default) keeps the stock PlatformKit chrome.
func WithBranding(resolver portslib.BrandingResolver) Option {
	return func(c *config) { c.branding = resolver }
}
