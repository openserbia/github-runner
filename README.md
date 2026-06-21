<!--
SPDX-FileCopyrightText: 2026 OpenSerbia
SPDX-License-Identifier: MIT
-->
# github-runner

[![build](https://github.com/openserbia/github-runner/actions/workflows/build.yml/badge.svg)](https://github.com/openserbia/github-runner/actions/workflows/build.yml)
[![lint](https://github.com/openserbia/github-runner/actions/workflows/lint.yml/badge.svg)](https://github.com/openserbia/github-runner/actions/workflows/lint.yml)
[![image size](https://img.shields.io/badge/image%20size-455%20MiB-2496ed?logo=docker&logoColor=white)](https://github.com/openserbia/github-runner/pkgs/container/github-runner)
[![Trivy: CRITICAL-gated](https://img.shields.io/badge/Trivy-CRITICAL--gated-1904da?logo=aqua&logoColor=white)](SECURITY.md)
[![cosign: signed](https://img.shields.io/badge/cosign-signed-0a7bbb?logo=sigstore&logoColor=white)](#verify-an-image)
[![SBOM: CycloneDX](https://img.shields.io/badge/SBOM-CycloneDX-26a269)](#verify-an-image)
[![License: MIT](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)

A self-hosted **GitHub Actions runner** on **[Chainguard Wolfi](https://github.com/wolfi-dev)**
(glibc, daily-patched, minimal) — the `actions/runner` agent plus the CI toolchain
our workflows expect (**Go, Node LTS, go-task, devbox, docker compose**) and a
small **Go entrypoint** (layered `internal/` packages, zerolog logging) that
JIT-registers an ephemeral runner. No `myoung34`/Ubuntu base: the Go entrypoint
replaced its bash registration, so we assemble the runner directly on Wolfi.

Published **multi-arch** to GHCR and **rebuilt weekly**:

| Arch | Built on |
|---|---|
| amd64 | self-hosted `X64` runner |
| arm64 | self-hosted `ARM64` runner |

> **Why no `armv7`?** That's the ceiling, and Wolfi is the binding constraint.
> The `actions/runner` agent *does* ship a 32-bit `linux-arm` (armv7) build, but
> **Chainguard Wolfi publishes only `amd64` + `arm64`** — there's no `armv7`
> `wolfi-base` to build `FROM`. Adding armv7 would mean dropping Wolfi for that
> arch and reintroducing the kernel-header CVE noise this image exists to avoid,
> so it's intentionally out.

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

The trigger was a scan that flagged the old Ubuntu-based runner with a wall of
"CRITICAL" CVEs — **~11 of 12 of which were `linux-libc-dev` kernel-header
findings that don't even apply to a container** (it uses the host kernel) and
have no upstream fix. The real signal was drowning in noise. This repo is the
fix: a **single, multi-arch, weekly-rebuilt, Trivy-gated, cosign-signed** image
on a minimal Wolfi base — whose scan only ever reports things you can actually
act on (the kernel-header class simply doesn't exist on Wolfi).

## Architecture

- **Multi-arch, one source.** One `Dockerfile`, built natively per arch on
  self-hosted runners, stitched into a single `:latest` manifest list. One place
  to bump; one Trivy row for the whole fleet.
- **Weekly scheduled rebuild** pulls a fresh, daily-patched `wolfi-base` and the
  latest `apk` packages on a cadence, not by accident. (No `apt upgrade` step and
  no git-lfs recompile — Wolfi ships current packages, and its `git-lfs` is
  already built against a patched Go.)
- **Go registration entrypoint** ([`cmd/runner-entrypoint`](cmd/runner-entrypoint),
  with logic in `internal/{config,observability,githubapi,runner}` and tests).
  A small static Go binary replaces the bash registration glue: it sets up
  git/registry/Go-module auth, registers an **ephemeral** runner via the GitHub
  [JIT-config API](https://docs.github.com/rest/actions/self-hosted-runners#create-configuration-for-a-just-in-time-runner-for-an-organization)
  (replacing a stale same-named registration on conflict), then `exec`s
  `bin/Runner.Listener` directly under `dumb-init` so signals reach the agent. The
  ephemeral "loop" is the container restart policy — one job per registration.

### The CVE-scan design

On Wolfi the `linux-libc-dev` kernel-header CRITs simply **don't exist** (Wolfi
doesn't ship them) and the OS-package surface is minimal + daily-patched, so the
gate is meaningful out of the box. The scan still uses **`--ignore-unfixed`**
(report only actionable findings) and **fails the build on a fixable CRITICAL**.

What remains is the `actions/runner` agent's own **bundled** dependencies — its
vendored `node20`/`node24` (tar/minimatch/glob) and the `docker-cli` Go binaries
— which are identical on any base and only clear when upstream ships a newer
agent / Wolfi rebuilds. A small [`.trivyignore`](.trivyignore) holds CVEs whose
Trivy-listed "fix" isn't actually shipped by the Wolfi repo (currently one
`openssl-config` config-package mis-match), each with a documented reason.

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
