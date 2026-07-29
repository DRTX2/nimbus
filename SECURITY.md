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

- Local mode bootstraps one tenant and owner key; additional API keys are hashed,
  expiring, revocable, role-bound, and do not reactivate after revocation.
- RBAC, separation-of-duty approvals, audit events, and tenant RLS policies exist.
  Current deployments use the schema owner, so RLS is not an active boundary
  until dedicated API/worker roles set tenant context per transaction.
- OIDC/JWKS and a general encrypted connector secret provider are not implemented.
  AWS AssumeRole external IDs are resolved indirectly from an environment reference.
- The AWS sandbox is single-node and exposes the app only through an SSM tunnel.

Cloud deployment, privileged connectors, and destructive operations always
require explicit human approval. Never commit `.env`, Terraform state, AWS keys,
tokens, database dumps, or real workflow payloads.
