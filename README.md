<!--
SPDX-FileCopyrightText: 2026 OpenSerbia
SPDX-License-Identifier: MIT
-->
# github-runner

[![build](https://github.com/openserbia/github-runner/actions/workflows/build.yml/badge.svg)](https://github.com/openserbia/github-runner/actions/workflows/build.yml)
[![lint](https://github.com/openserbia/github-runner/actions/workflows/lint.yml/badge.svg)](https://github.com/openserbia/github-runner/actions/workflows/lint.yml)
[![Trivy: CRITICAL-gated](https://img.shields.io/badge/Trivy-CRITICAL--gated-1904da?logo=aqua&logoColor=white)](SECURITY.md)
[![cosign: signed](https://img.shields.io/badge/cosign-signed-0a7bbb?logo=sigstore&logoColor=white)](#verify-an-image)
[![SBOM: CycloneDX](https://img.shields.io/badge/SBOM-CycloneDX-26a269)](#verify-an-image)
[![License: MIT](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)

A self-hosted **GitHub Actions runner** image:
[`myoung34/github-runner`](https://github.com/myoung34/docker-github-actions-runner)
(Ubuntu Noble) plus the CI toolchain our workflows expect — **Go, Node LTS,
go-task, devbox, docker compose** — and a small **Go entrypoint** (layered
`internal/` packages, zerolog logging) that JIT-registers an ephemeral runner.

Published **multi-arch** to GHCR and **rebuilt weekly**:

| Arch | Built on |
|---|---|
| amd64 | self-hosted `X64` runner |
| arm64 | self-hosted `ARM64` runner |

```
ghcr.io/openserbia/github-runner:latest             # rolling multi-arch (amd64 + arm64)
ghcr.io/openserbia/github-runner@sha256:<digest>    # pin an immutable build (cosign-signed)
```

There is no dated tag — `:latest` is the only rolling tag. For a reproducible,
immutable reference, pin by `@sha256:` digest (every digest is cosign-signed; see
[Verify an image](#verify-an-image)).

## Why I built this

The runner image was previously built ad-hoc on each host (`docker compose build
--pull` whenever a replica was recreated). Three problems made that untenable:

1. **It rotted.** Between recreates the image drifted weeks behind on patches,
   with nothing rebuilding it on a cadence.
2. **No build-time CVE gate.** Vulnerabilities were discovered by a *post-deploy*
   weekly scan — after the image was already live — rather than blocked at build.
3. **Per-arch duplication.** Each architecture (and each host) built its own
   near-identical image, multiplying disk, build time, and scan noise.

The trigger was a scan that flagged the runner with a wall of "CRITICAL" CVEs —
**~11 of 12 of which were `linux-libc-dev` kernel-header findings that don't even
apply to a container** (it uses the host kernel) and have no upstream fix. The
real signal was drowning in noise. This repo is the fix: a **single, multi-arch,
weekly-rebuilt, Trivy-gated, cosign-signed** image whose scan only ever reports
things you can actually act on.

## Architecture

- **Multi-arch, one source.** One `Dockerfile`, built natively per arch on
  self-hosted runners, stitched into a single `:latest` manifest list. One place
  to bump; one Trivy row for the whole fleet.
- **Weekly scheduled rebuild** pulls a fresh base + `apt upgrade` (the
  patch-delivery channel) on a cadence, not by accident.
- **Go registration entrypoint** ([`cmd/runner-entrypoint`](cmd/runner-entrypoint),
  with logic in `internal/{config,observability,githubapi,runner}` and tests).
  A small static Go binary replaces the bash registration glue: it sets up
  git/registry/Go-module auth, registers an **ephemeral** runner via the GitHub
  [JIT-config API](https://docs.github.com/rest/actions/self-hosted-runners#create-configuration-for-a-just-in-time-runner-for-an-organization),
  then `exec`s the agent's `run.sh` so signals reach the runner directly. The
  ephemeral "loop" is the container restart policy — one job per registration.

### The CVE-scan design (important)

The Ubuntu base carries ~11 CRITICAL CVEs in **`linux-libc-dev`** (kernel
headers). These **don't apply to a container** — it uses the *host* kernel; the
headers are compile-time only — and they're `fixed=none` upstream, so no rebuild
can clear them. The scan therefore uses **`--ignore-unfixed`**: the gate fails
only on CRITICALs that actually **have a fix**.

That leaves exactly one fixable CRITICAL: **CVE-2025-68121**, a Go-stdlib issue
baked into the pre-compiled `git-lfs` 3.7.1 binary. git-lfs has no newer release,
so the Dockerfile **recompiles git-lfs 3.7.1 from source against the Go 1.26.4 we
already install** (patched stdlib) and overwrites the base binary in place —
Trivy scans the file on disk, so shadowing via `PATH` is not enough. The smoke
test asserts git-lfs reports `go >= 1.26`, so the fix can't silently regress.

## Verify an image

Every pushed image (and the multi-arch index) is **keyless cosign-signed** via
GitHub OIDC:

```bash
cosign verify ghcr.io/openserbia/github-runner:latest \
  --certificate-identity-regexp '^https://github.com/openserbia/github-runner/' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
```

A CycloneDX **SBOM** is attested to each image:

```bash
cosign download attestation ghcr.io/openserbia/github-runner:latest
```

## Local development

Container tooling comes from [devbox](https://www.jetify.com/devbox) (`go-task`,
`trivy`, `syft`, `cosign`); the Go entrypoint builds with the standard toolchain:

```bash
devbox run -- task ci     # build -> scan -> sbom -> smoke (no push)
devbox run -- task scan   # Trivy gate (fail on fixable CRITICAL)
devbox run -- task smoke  # boot + assert toolchain, git-lfs fix, Go entrypoint
go test ./...             # unit-test the registration entrypoint
```

The image build itself runs `go vet` + `go test` in the builder stage, so a
broken entrypoint fails the build — no separate CI leg.

## How it's consumed

Point each host's `docker-compose.yml` at `ghcr.io/openserbia/github-runner`
instead of a locally-built tag. The runner name/labels, replica count, and host
mounts stay in your own deployment config — this image is the **generic** runtime.

```yaml
services:
  github-runner:
    image: ghcr.io/openserbia/github-runner:latest
    # Ephemeral by construction: the Go entrypoint JIT-registers, runs ONE job,
    # exits, and `restart` re-registers a fresh runner. No EPHEMERAL flag needed.
    restart: unless-stopped
    environment:
      ORG_NAME: your-org                  # required — org the runner joins
      RUNNER_SCOPE: org                   # required — only org scope is supported
      RUNNER_NAME: runner-1               # required — unique per replica
      RUNNER_LABELS: self-hosted-x64,docker  # required — your custom labels
      ACCESS_TOKEN: ${ACCESS_TOKEN}       # required — PAT with org runner-admin (registers + replaces)
      GITHUB_PAT: ${GITHUB_PAT}           # optional — git/Go-module auth for private repos
      # RUNNER_GROUP_ID: "1"              # optional — runner group (default: Default group)
      # LOG_LEVEL: info                   # optional — zerolog level
      # Talk to the HOST Docker daemon for `docker build` / compose in jobs:
      DOCKER_HOST: unix:///host-run/docker.sock
    security_opt:
      - no-new-privileges:true
    volumes:
      # DIRECTORY mount of host /run (NOT the socket file) so the runner survives
      # a dockerd restart; the agent connects to /host-run/docker.sock.
      - /run:/host-run:ro
      - runner-work:/actions-runner/_work

volumes:
  runner-work:
```

Put the secrets (`ACCESS_TOKEN`, `GITHUB_PAT`) in an `.env` file or your secret
store — never in the compose file. Run N replicas by giving each a distinct
`RUNNER_NAME`. `ACCESS_TOKEN` needs the org **self-hosted-runners** admin scope;
`GITHUB_PAT` only needs read access to the private repos your jobs pull.

Pull image refreshes on a **drain-aware** schedule (recreate a replica only when
it's idle — recreating a runner mid-job kills that job), not via naive Watchtower.

## License

[MIT](LICENSE).
