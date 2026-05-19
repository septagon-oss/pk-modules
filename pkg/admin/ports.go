package admin

// ports.go owns the public interfaces other modules consume from this module.
// The admin module is the canonical provider of portslib.AdminRegistrar; this
// file only exists to keep the symmetry with sibling modules (audit, user,
// notification) where ports.go declares the public Service contracts.
//
// The portslib.AdminRegistrar contract itself lives in
// github.com/septagon-oss/pk-modules/pkg/portslib so callers can satisfy it
// without depending on this implementation package.
//
// ADR: ADR-0009 (ports-only module communication), ADR-0029 (file purpose declaration).
// Convention: C-14 (every Go file declares its purpose).

import "github.com/septagon-oss/pk-modules/pkg/portslib"

// Registrar is a convenience alias documenting that *Shell implements
// portslib.AdminRegistrar. Callers should depend on the interface, not the
// concrete type, but the alias makes the contract grep-discoverable.
type Registrar = portslib.AdminRegistrar
