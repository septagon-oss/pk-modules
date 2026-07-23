# pk-modules Charter

## Purpose

Reference OSS modules that prove PlatformKit's public contracts. Each module is a self-contained vertical slice: store interface, SQLite implementation, HTTP handlers, and migrations.

## In Scope

- Tenant management: multi-tenant isolation, host alias resolution, workspace management
- User management: user lifecycle, profile, preferences, registration
- Authentication: login, registration, MFA, session management, password reset
- API key management: key creation, validation, rotation, revocation, rate limiting
- Content management: articles, categories, publish lifecycle, RSS feeds
- Notification management: in-app and email notification channels
- Audit management: audit trails, event capture, compliance checks
- Admin management: admin interface, dashboard, settings
- Health monitoring: aggregated health checks, alert derivation

## Out of Scope

- UI components or admin renderers (consumed as ports)
- Cloud-specific integrations (SES, SNS, etc.)
- Enterprise authentication providers (LDAP, SAML, OIDC — live in Pro)
- Billing provider adapters (Stripe, etc. — live in Pro)
- Deployments or operations automation

## Dependencies

- `github.com/septagon-oss/pk-core` — module, authz, entity, event contracts
- `modernc.org/sqlite` — embeddable SQLite for OSS reference implementations
