# Security Policy

## Reporting

Do not open public issues for suspected vulnerabilities. Until a private
security contact is configured, use GitHub private vulnerability reporting on
the repository. Do not include production credentials or customer payloads.

## Supported Versions

Nimbus is pre-1.0. Only the current `main` branch receives security fixes.

## Threat Model Summary

Primary risks are cross-tenant access, credential disclosure, duplicate external
effects, SSRF from HTTP actions, queue exhaustion, stale worker writes, and
software supply-chain compromise.

Implemented mitigations are documented in the README and code. The following
limitations mean this release must not host mutually untrusted tenants:

- The one-shot migrator bootstraps one local tenant and owner key; API and worker
  processes only verify immutable migration checksums. Additional API keys are
  hashed, expiring, revocable, role-bound, and do not reactivate after revocation.
- Compose and Kubernetes support distinct non-owner API and worker database
  roles. Tenant tables force RLS; API policy resolves an opaque authenticated
  key capability rather than trusting a tenant setting, while worker tenant
  context remains transaction-local. Runtime roles cannot alter audit events or
  read migration metadata directly. Production operators must provision those roles
  before migration; Nimbus never creates roles or embeds production passwords.
- OIDC tokens are verified against the provider's discovery document with an
  algorithm allowlist, issuer, audience and expiry checks, then exchanged for a
  short-lived database session bound to RLS exactly like an API key. First sign-in
  provisions a viewer; role or status changes end the sessions immediately, and
  verified tokens are cached for at most one minute.
- A general encrypted connector secret provider is not implemented. AWS
  AssumeRole external IDs are resolved indirectly from an environment reference.
- Webhook deliveries are HMAC-signed (`Nimbus-Signature: t=...,v1=...`) with
  support for a previous secret during rotation; receivers must enforce a
  timestamp window.
- The dashboard is same-origin, served with a strict CSP, keeps credentials in
  sessionStorage only, and never stores them in localStorage or cookies.
- The AWS sandbox is single-node and exposes the app only through an SSM tunnel.

Cloud deployment, privileged connectors, and destructive operations always
require explicit human approval. Never commit `.env`, Terraform state, AWS keys,
tokens, database dumps, or real workflow payloads.
