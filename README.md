# pk-modules

Starter OSS module pack for PlatformKit.

This repo contains generic modules that demonstrate the public PlatformKit
module contract. It should stay useful without becoming a product dump:
community modules can follow these patterns, while vertical modules, client
overlays, and hosted operational modules belong in Pro/private packs.

The repo also publishes `.platformkit/docs.manifest.yaml` so `pk-docs` can
federate the module-pack overview through the same public documentation model
used by downstream module packs.

## Current Surface

- `pkg/coremodules`: tenant, audit, and content example modules

## Verify

```bash
make verify
make staticcheck
```
