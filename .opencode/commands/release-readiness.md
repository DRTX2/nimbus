---
description: Assess Nimbus release readiness without publishing or deploying.
agent: build
---

Load `nimbus-supply-chain-review`. Review tests, race results, vulnerabilities,
pins, image hardening, SBOM/provenance, migrations, rollback, and documentation.
Return blockers and a verdict. Do not publish or deploy.

Release candidate: $ARGUMENTS
