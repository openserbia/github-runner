<!--
SPDX-FileCopyrightText: 2026 OpenSerbia
SPDX-License-Identifier: MIT
-->
# Security Policy

`github-runner` is a self-hosted CI runner image. It runs trusted OpenSerbia-org
workflows on self-hosted amd64 + arm64 pools; reducing its CVE surface and
keeping it fresh is the point of the project.

## Reporting a vulnerability

**Do not open a public GitHub Issue for a security vulnerability.**

Report privately via **[GitHub Security Advisories](https://github.com/openserbia/github-runner/security/advisories/new)**
("Report a vulnerability"), or email **charnyshevich.job@gmail.com** with subject
`github-runner security`.

Please include the affected ref (`:latest` or a `@sha256:` digest) and the digest
if known, the issue and its impact, and reproduction steps where possible.

## Scope

This project's own artifacts: the `Dockerfile`, build/release CI, the image
assembly (toolchain selected, git-auth wrapper), and the signing/SBOM pipeline.
Vulnerabilities in **upstream** components (the `myoung34` base, the Actions
runner agent, Ubuntu packages, Go/Node/git-lfs) should go to those projects; we
pull their fixes in the next weekly rebuild and, for fixable CRITICALs, out of
band.

## Security model — what you can and cannot expect

**You can expect:**

- A multi-arch (amd64 + arm64) image on a minimal, **daily-patched Chainguard
  Wolfi** base, **rebuilt weekly** so fresh `apk` packages flow through on a cadence.
- A **Trivy-gated** build that **fails on fixable CRITICAL** CVEs
  (`--ignore-unfixed`); HIGH + unfixable findings are reported, non-gating.
- A CycloneDX **SBOM** attested to every image, and **keyless cosign signatures**
  (Sigstore / GitHub OIDC) on every pushed image and the multi-arch index.
- **No secrets baked into the image** — git/registry auth (`GITHUB_PAT`,
  `~/.docker/config.json`) is supplied at runtime by the Go entrypoint.

**You cannot expect:**

- Remediation of the `actions/runner` agent's **bundled** dependencies (its
  vendored `node20`/`node24`, .NET libs) — they're identical on any base and
  clear only when GitHub ships a newer agent.
- A fix for CVEs whose Trivy-listed "fixed" version isn't actually shipped by the
  Wolfi repo — these are held in [`.trivyignore`](.trivyignore) with a reason and
  revisited when Wolfi rebuilds.
- Hardening of the *workflows* that run on the runner, or of the host Docker
  daemon the runner talks to — those are the operator's responsibility.
- A patched image for HIGH/MEDIUM CVEs faster than the weekly cadence (only
  fixable CRITICALs are expedited).

## Supported tags

| Ref | Supported |
|---|---|
| `:latest` | ✅ rebuilt weekly (the only rolling tag) |
| `@sha256:<digest>` | ✅ immutable pin; repull `:latest` for fixes |
