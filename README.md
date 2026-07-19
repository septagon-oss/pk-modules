# pk-modules

[![Go Reference](https://pkg.go.dev/badge/github.com/septagon-oss/pk-modules.svg)](https://pkg.go.dev/github.com/septagon-oss/pk-modules)
[![CI](https://github.com/septagon-oss/pk-modules/actions/workflows/go.yml/badge.svg)](https://github.com/septagon-oss/pk-modules/actions/workflows/go.yml)
[![License: Apache-2.0](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE)

`pk-modules` is the starter OSS module pack for the open-source PlatformKit family. It ships small, self-contained business modules — tenant, user, auth, API key, content, notification, audit, admin, and health — that demonstrate the public PlatformKit module contract end to end: an entity, a `store.Store` persistence port with a SQLite reference implementation, a service, and an HTTP handler. Each module is wired with functional options and composes into a host application through pk-core's dependency-injection bundle, so community modules can follow the same patterns while vertical, client, and hosted-operational modules live in Pro/private packs.

## Install

```bash
go get github.com/septagon-oss/pk-modules@v0.1.0
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
- `pkg/admin`, `pkg/health` — a minimal self-contained admin shell and health/readiness reporting.
- Every data module exposes a `store.Store` persistence port plus a `store/sqlite` reference implementation built on `modernc.org/sqlite` (pure-Go, no cgo).
- `pkg/coremodules` — the smallest composable bundle wiring tenant + audit + content for OSS examples and downstream distributions.
- `pkg/portslib` — the shared port contracts modules consume explicitly instead of importing one another.

## Verify

```bash
make verify   # go test + go vet + staticcheck + race
```

## License

Apache-2.0. See [LICENSE](LICENSE).
