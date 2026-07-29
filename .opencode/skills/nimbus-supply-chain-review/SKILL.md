---
name: nimbus-supply-chain-review
description: Use ONLY when changing go.mod, go.sum, Dockerfiles, CI, build tools, generated code, releases, GitHub Actions, images, or dependency versions.
---

# Nimbus Supply Chain Review

- Do not use `curl | sh`, `go install ...@latest`, or unreviewed remote installers.
- Prefer stdlib or an existing dependency before adding one.
- Record the need, alternatives, maintainer, license, and exact version.
- Pin Go modules, tools, CI actions, and release images; use digests for production images.
- Keep `GOSUMDB` enabled for public modules and run `go mod verify`.
- Review the complete `go.mod` and `go.sum` diff.
- Scan Go code and container images with project-approved, pinned tools.
- Generate an SBOM and provenance for releases when the release pipeline is enabled.
- Never print environment variables, credentials, or secret files in CI.
- Require human approval for publication, production deployment, and credential rotation.
