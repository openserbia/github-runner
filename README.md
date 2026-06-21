<!--
SPDX-FileCopyrightText: 2026 OpenSerbia
SPDX-License-Identifier: MIT
-->
# github-runner

A self-hosted **GitHub Actions runner** image for the OpenSerbia fleet:
[`myoung34/github-runner`](https://github.com/myoung34/docker-github-actions-runner)
(Ubuntu Noble) plus the CI toolchain our workflows expect — **Go, Node LTS,
go-task, devbox, docker compose** — and a git-auth entrypoint wrapper.

Published **multi-arch** to GHCR and **rebuilt weekly**, so a single image serves
both pools:

| Pool | Arch | `runs-on` |
|---|---|---|
| AX41 (Hetzner) | amd64 | `[self-hosted, ax41]` |
| rpi-server | arm64 | `[self-hosted, rpi]` |

```
ghcr.io/openserbia/github-runner:latest      # rolling multi-arch (amd64 + arm64)
ghcr.io/openserbia/github-runner:YYYYMMDD     # immutable dated snapshot
```

## Why this exists

The runner image used to be built ad-hoc on the box (`docker compose build
--pull` whenever a replica was recreated). That left it **stale between
recreates**, gave **no build-time CVE gate**, and meant AX41 and the rpi each
built their **own** near-identical image. This repo fixes all three:

- **Weekly scheduled rebuild** pulls a fresh base + `apt upgrade` (the
  patch-delivery channel) on a cadence, not by accident.
- **Trivy CRITICAL gate** at build time — a fixable CRITICAL fails the build, so
  a vulnerable image never publishes.
- **One multi-arch image** for the whole fleet — one place to bump, one Trivy
  row for six runners.

### The CVE-scan design (important)

The Ubuntu base carries ~11 CRITICAL CVEs in **`linux-libc-dev`** (kernel
headers). These **don't apply to a container** — it uses the *host* kernel; the
headers are compile-time only — and they're `fixed=none` upstream, so no rebuild
can clear them. The scan therefore uses **`--ignore-unfixed`**: the gate fails
only on CRITICALs that actually **have a fix**.

That leaves exactly one fixable CRITICAL: **CVE-2025-68121**, a Go-stdlib issue
baked into the pre-compiled `git-lfs` 3.7.1 binary. git-lfs has no newer release,
so the Dockerfile **recompiles git-lfs 3.7.1 from source against the Go 1.26.4 we
already install** (patched stdlib) and overwrites the base binary in place. The
smoke test asserts git-lfs reports `go >= 1.26`, so the fix can't silently
regress.

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

Tooling comes from [devbox](https://www.jetify.com/devbox) (`go-task`, `trivy`,
`syft`, `cosign`):

```bash
devbox run -- task ci     # build -> scan -> sbom -> smoke (no push)
devbox run -- task build  # build :latest + :DATE
devbox run -- task scan   # Trivy gate (fail on fixable CRITICAL)
devbox run -- task smoke  # boot + assert toolchain & git-lfs CVE fix
```

## How it's consumed

Both pools' `docker-compose.yml` reference `ghcr.io/openserbia/github-runner`
instead of a locally-built tag. The runner labels (`ax41` / `rpi`), replica
count, and the host `/run` + services mounts stay in each host's compose file —
this image is the **generic** runtime; the per-host wiring lives in
[`~/.setup`](https://github.com/OCharnyshevich) (NixOS infra repo).

Image refreshes are pulled on a **drain-aware** schedule (recreate only when all
replicas are idle — recreating a runner mid-job kills that job), not by naive
Watchtower.

## License

[MIT](LICENSE).
