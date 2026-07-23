# pk-modules

> Part of [PlatformKit](https://github.com/septagon-oss/platformkit) — the open-source Go backend for multi-tenant SaaS.

**Depends on.** `pk-core` only within PlatformKit (plus `modernc.org/sqlite` for the reference stores). It does not depend on `pk-shared` or `pk-runtime` — an app supplies the host.

[![Go Reference](https://pkg.go.dev/badge/github.com/septagon-oss/pk-modules.svg)](https://pkg.go.dev/github.com/septagon-oss/pk-modules)
[![CI](https://github.com/septagon-oss/pk-modules/actions/workflows/go.yml/badge.svg)](https://github.com/septagon-oss/pk-modules/actions/workflows/go.yml)
[![License: Apache-2.0](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE)

`pk-modules` is the starter OSS module pack for the open-source PlatformKit family. It ships small, self-contained business modules — tenant, user, auth, API key, content, notification, audit, admin, and health — that demonstrate the public PlatformKit module contract end to end: an entity, a `store.Store` persistence port with a SQLite reference implementation, a service, and an HTTP handler. Each module is wired with functional options and composes into a host application through pk-core's dependency-injection bundle, so community modules can follow the same patterns while vertical, client, and hosted-operational modules live in Pro/private packs.

## Install

```bash
go get github.com/septagon-oss/pk-modules@latest
```

## Usage

```go
package main

import (
	"context"
	"log"
	"net/http"

	"github.com/septagon-oss/pk-modules/pkg/tenant"

	// Register the reference SQLite driver (modernc.org/sqlite) under the
	// default "sqlite" name the modules open against.
	_ "modernc.org/sqlite"
)

func main() {
	// Construct a module backed by a SQLite database. The store auto-creates
	// its schema, so a fresh DSN is ready to use immediately.
	m, err := tenant.NewModule(tenant.WithSQLiteDSN("file:tenants.db"))
	if err != nil {
		log.Fatal(err)
	}

	// Drive the module through its public service port.
	ctx := context.Background()
	t := &tenant.Tenant{Slug: "acme", Name: "Acme Inc."}
	if err := m.Service().Create(ctx, t); err != nil {
		log.Fatal(err)
	}

	// Mount the module's CRUD handler onto any net/http server.
	http.Handle("/api/v1/tenants/", m.HTTPHandler())
	log.Fatal(http.ListenAndServe(":8080", nil))
}
```

## Current Surface

- `pkg/tenant`, `pkg/user`, `pkg/auth`, `pkg/apikey` — identity, access, and session primitives.
- `pkg/content`, `pkg/notification`, `pkg/audit` — content publishing, in-app notifications, and append-only audit logging.
- `pkg/admin`, `pkg/health` — a responsive, accessible reference admin and
  health/readiness reporting. Modules register an `AdminResource` contract
  with readable columns, typed fields, lifecycle actions, and allowed
  operations; the shell renders real tables and forms without a raw JSON
  editor or frontend build step.
- Every data module exposes a `store.Store` persistence port plus a `store/sqlite` reference implementation built on `modernc.org/sqlite` (pure-Go, no cgo).
- `pkg/portslib` — the shared port contracts modules consume explicitly instead of importing one another.

## Version namespaces

Each module exposes two deliberately separate versions:

- `ReleaseVersion` is the `pk-modules` release shown in catalog/runtime
  metadata. It is `0.4.0` for this review build.
- `ModuleVersion` remains the module's port-contract version, so an unrelated
  release does not invalidate compatible third-party modules. Existing
  contracts remain at `0.0.0`.

The schema-aware `AdminRegistrar` API is the exception because it replaces the
older `RegisterEntityCRUD` interface. Its contract is `0.4.0`; consumers should
declare `portslib.AdminRegistrarContractVersion` instead of copying a version
literal.

## Verify

```bash
make verify   # go test + go vet + staticcheck + race
```

## License

Apache-2.0. See [LICENSE](LICENSE).
