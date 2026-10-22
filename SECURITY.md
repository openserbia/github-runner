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
assembly (toolchain selected, git-auth wrapper), and the signing/SBOM/provenance
pipeline. Vulnerabilities in **upstream** components (Wolfi packages, the Actions
runner agent, Node, devbox, git-lfs) should go to those projects; we pull their
fixes in the next rebuild and, for fixable CRITICALs, out of band.

## Security model — what you can and cannot expect

**You can expect:**

- A multi-arch (amd64 + arm64) image on a minimal, **daily-patched Chainguard
  Wolfi** base, **rebuilt weekly** (`apk upgrade` included) so fixed packages flow
  through on a cadence.
- A build on **GitHub-hosted** runners, never on the self-hosted pool the image
  becomes, so a compromised runner can't backdoor the next runner image.
- **Verified inputs:** the runner archive and Node are SHA-256 checked against
  their publishers' checksums; devbox is built from a commit-pinned tag with our
  patched Go; Wolfi packages come signed from Wolfi's repo.
- **Fresh pins:** `bump.yml` opens a PR whenever the runner, Node 24 or devbox
  moves (GitHub refuses jobs on runners ~30 days behind, and auto-update is off),
  and Dependabot covers actions, base images and Go modules with a 7-day cooldown.
- A **Trivy-gated** build that **fails on fixable CRITICAL** CVEs
  (`--ignore-unfixed`); HIGH + unfixable findings are reported, non-gating.
- **Keyless cosign signatures** (Sigstore / GitHub OIDC), a CycloneDX **SBOM**
  attestation, and **SLSA build provenance** on every pushed image and the
  multi-arch index (see [Verifying an image](#verifying-an-image)).
- **No secrets baked into the image** — git/registry auth is supplied at runtime
  by the Go entrypoint, which also **strips `ACCESS_TOKEN` and `GITHUB_PAT` from
  the environment jobs inherit** (jobs get the PAT only via git's URL rewrite and
  `GITHUB_TOKEN_FILE`).
- Workflow changes audited by **zizmor** on every PR.

**You cannot expect:**

- Remediation of the `actions/runner` agent's **bundled .NET** runtime and libs —
  they clear only when GitHub ships a newer agent (`bump.yml` picks it up the
  day it does). Likewise npm's own bundled deps inside the pinned Node build.
- A fix for CVEs whose Trivy-listed "fixed" version isn't actually shipped by the
  Wolfi repo — these are held in [`.trivyignore`](.trivyignore) with a reason and
  revisited when Wolfi rebuilds.
- Hardening of the *workflows* that run on the runner — the operator's
  responsibility (the org requires SHA-pinned actions and restricts which repos
  may use the runner group).
- A patched image for HIGH/MEDIUM CVEs faster than the weekly cadence (only
  fixable CRITICALs are expedited).

## Accepted risks (deployment)

These follow from the deliberate deployment model (jobs build app images on the
host daemon and Watchtower deploys them from Docker events, with no registry) and
are accepted, not overlooked:

- **Jobs are root-equivalent on the host.** Runners reach the host Docker daemon
  through its socket, so any job can start a privileged container, read other
  containers' environment (`docker inspect`), including the runners' own
  `ACCESS_TOKEN`, and replace locally built app images that Watchtower deploys.
  Stripping secrets from the job environment only stops casual leaks (env
  dumps, install-script harvesters), not a deliberate attacker. Mitigations: only
  trusted, SHA-pinned workflows run here; fork PRs never reach the pool; the
  runner group is limited to named repos; `ACCESS_TOKEN` should be a
  fine-grained token limited to *Self-hosted runners*.
- **Runners of different trust share a daemon** (on AX41, a second org's runner
  uses the same host socket), so a job in either org can reach the other's
  containers and secrets.
- **Jobs run as root inside the runner container.** With the host socket
  mounted, a non-root job user would add little; revisit if the socket goes.
- **`_work` and `/nix` persist across jobs** (named volumes, for build caches and
  the `--volumes-from` workflow pattern); registration is per-job, the
  filesystem is cleared by the host's periodic reset.

## Supported tags

| Ref | Supported |
|---|---|
| `:latest` | ✅ rebuilt weekly (the only rolling tag) |
| `@sha256:<digest>` | ✅ immutable pin; repull `:latest` for fixes |

## Verifying an image

```bash
# Signature (keyless, this repo's workflows only)
cosign verify ghcr.io/openserbia/github-runner:latest \
  --certificate-identity-regexp '^https://github.com/openserbia/github-runner/' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com

# SLSA build provenance (built by this repo, on GitHub-hosted runners)
gh attestation verify oci://ghcr.io/openserbia/github-runner:latest \
  --repo openserbia/github-runner
```
