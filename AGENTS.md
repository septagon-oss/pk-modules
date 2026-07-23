# Agent orientation

`pk-modules` contains the nine real, reusable OSS module implementations used
by the canonical PlatformKit starter. It is a library repository, not a
runnable application or a product template.

## Canonical paths

- Module implementations: `pkg/tenant`, `pkg/user`, `pkg/auth`, `pkg/apikey`,
  `pkg/audit`, `pkg/content`, `pkg/notification`, `pkg/admin`, and `pkg/health`.
- Shared module-facing contracts: `pkg/portslib`.
- Canonical runnable application:
  [septagon-oss/platformkit](https://github.com/septagon-oss/platformkit).
- Canonical composition package:
  `github.com/septagon-oss/pk-apps/pkg/starterapp`.

Do not add teaching bundles, sample domains, client concepts, fake product
modules, or alternate starter applications here. Product-specific modules
belong in the downstream product repository and integrate through published
ports.

Before submitting changes, run `make verify`. Existing migrations are
append-only, and every module must preserve tenant isolation and server-owned
request identity.
