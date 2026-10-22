# SPDX-FileCopyrightText: 2026 OpenSerbia
# SPDX-License-Identifier: MIT
# github-runner — a GitHub Actions self-hosted runner on Chainguard Wolfi (glibc,
# daily-patched, minimal) + devbox, docker (CLI + buildx), git/git-lfs and gh,
# and a Go JIT-registration entrypoint (cmd/runner-entrypoint). Published
# multi-arch (amd64 + arm64).
#
# No Go, go-task, yq or docker compose, and no separate Node: every openserbia
# workflow on these runners gets its toolchain from the repo's devbox.json
# (`devbox run -- task ...`), so image copies were dead weight (~280 MiB).
# `node` on PATH is the runner's own bundled node24.
#
# The actions/runner agent bundles its OWN trimmed .NET runtime + node24 (node20 pruned below);
# we add only the native libs it loads (icu/krb5/openssl/zlib/lttng-ust/...).
# Because the Go entrypoint replaced myoung34's bash registration, we don't need
# the myoung34/Ubuntu base at all — Wolfi drops the linux-libc-dev kernel-header
# CVE noise, the `apt upgrade` step, AND the git-lfs recompile (Wolfi's git-lfs
# is already built against a patched Go).
#
# :latest is deliberate — wolfi-base is daily-patched; pinning the base by digest
# would freeze out those patches. Dependabot bumps the FROM via PR instead.

# ---- entrypoint builder ----------------------------------------------------
# Compiles the Go registration entrypoint to a static, dependency-free binary
# (`go test`/`go vet` run here too, so a failure fails the image build).
# Cache mounts keep the module and build caches across builds without baking
# them into a layer (GOPATH is /go in the golang image, HOME is /root).
FROM golang:1.27.3 AS entrypoint-build
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go vet ./... \
 && CGO_ENABLED=0 go test ./... \
 && CGO_ENABLED=0 go build -trimpath -tags netgo,osusergo -ldflags="-s -w" \
    -o /runner-entrypoint ./cmd/runner-entrypoint

# ---- runtime ---------------------------------------------------------------
# hadolint ignore=DL3007
FROM cgr.dev/chainguard/wolfi-base:latest

# Pinned (deliberate-bump) versions for the bits NOT delivered via Wolfi's repo.
ARG RUNNER_VERSION=2.338.0
# Node 24 that runs every JS action, replacing the runner's bundled node24 (see
# the runner RUN below). Stays on 24: the runner has no node26 handler.
ARG NODE_VERSION=24.22.1

# Runtime deps, installed first with the default /bin/sh (wolfi-base has no bash):
#  - native libs the bundled .NET runner loads (krb5/libssl3/zlib/lttng-ust/
#    libstdc++/libgcc/libicu78). libicu78 is Wolfi's current ICU; the generic
#    icu-libs package lags at ICU 75. .NET probes for any libicuuc.so.N, and
#    Runner.Listener aborts without ICU, which the smoke test catches.
#    libssl3 is explicit because .NET 8 loads libssl.so.3, and openssl now
#    resolves to openssl-4.0 (libssl.so.4); today only libldap pulls libssl3.
#  - tools workflows call outside devbox (git, git-lfs, docker-cli + buildx for
#    BuildKit `docker build`, gh, jq)
#  - bash/curl/xz/dumb-init/ca-certs for the entrypoint, downloads, devbox/Nix, TLS
# Wolfi's rolling repo IS the patch-delivery channel, so versions are unpinned.
# gnutar (GNU tar, overrides busybox tar) + zstd: actions/cache invokes
# `tar --posix` / zstd, which busybox tar can't do — without these, cache
# save/restore fails on every job.
# mount: the Nix installer devbox runs on first use calls `mount -f` when /nix
# is a mount point. Hosts persist /nix as a per-runner volume (so the store
# survives container recreates); without mount every devbox job fails there.
# No openssl-config: Wolfi's openssl now resolves to openssl-4.0, which ships
# /etc/ssl/openssl.cnf itself, and the separate openssl-config (3.3) package
# conflicts with its files, failing every build since 2026-10-09.
# hadolint ignore=DL3018
RUN apk add --no-cache \
      bash curl xz git jq gh gnutar zstd dumb-init ca-certificates-bundle \
      krb5-libs openssl libssl3 zlib libstdc++ libgcc lttng-ust libicu78 \
      git-lfs docker-cli docker-cli-buildx mount

# bash now exists → use it with pipefail so the curl|… downloads below fail fast.
SHELL ["/bin/bash", "-o", "pipefail", "-c"]

# devbox (binary only; Nix provisions on first `devbox run` inside a workflow).
RUN curl -fsSL https://get.jetify.com/devbox | bash -s -- -f

# actions/runner agent (arch-aware: x64 / arm64). Bundles its own .NET + nodes.
# Pruned in the same layer (a later RUN rm would not shrink the image):
#  - node20: past its 2026-09-23 removal date; FORCE_JAVASCRIPT_ACTIONS_TO_NODE24
#    below runs `using: node20` actions on node24 instead.
#  - node*_alpine: only used for JS actions inside Alpine `container:` jobs. The
#    agent bind-mounts its externals into job containers through the HOST
#    daemon, where /actions-runner does not exist, so they are unreachable here.
#  - node24: swapped for the official NODE_VERSION build (SHA-256 checked
#    against nodejs.org's SHASUMS256). Runner releases lag Node security
#    releases by weeks (v2.338.0 bundles 24.21.0, 24.22.1 is a security fix),
#    and the official build's npm carries current deps (e.g. a patched tar),
#    so neither waits for GitHub to republish the runner.
#  - node24/include + docs: C headers for native addons, never used at runtime.
#  - node24 is linked onto PATH as node/npm/npx for ad-hoc `run:` steps.
RUN case "$(uname -m)" in \
      x86_64) rarch=x64 ;; \
      aarch64) rarch=arm64 ;; \
      *) echo "unsupported arch: $(uname -m)" >&2; exit 1 ;; \
    esac \
 && mkdir -p /actions-runner \
 && curl -fsSL "https://github.com/actions/runner/releases/download/v${RUNNER_VERSION}/actions-runner-linux-${rarch}-${RUNNER_VERSION}.tar.gz" \
    | tar -C /actions-runner -xz \
 && rm -rf \
      /actions-runner/externals/node20 \
      /actions-runner/externals/node20_alpine \
      /actions-runner/externals/node24_alpine \
      /actions-runner/externals/node24 \
 && node_tar="node-v${NODE_VERSION}-linux-${rarch}.tar.xz" \
 && curl -fsSLO "https://nodejs.org/dist/v${NODE_VERSION}/${node_tar}" \
 && curl -fsSL "https://nodejs.org/dist/v${NODE_VERSION}/SHASUMS256.txt" \
    | grep " ${node_tar}\$" | sha256sum -c - \
 && mkdir /actions-runner/externals/node24 \
 && tar -C /actions-runner/externals/node24 --strip-components=1 -xJf "${node_tar}" \
 && rm -rf "${node_tar}" \
      /actions-runner/externals/node24/include \
      /actions-runner/externals/node24/share \
      /actions-runner/externals/node24/CHANGELOG.md \
      /actions-runner/externals/node24/README.md \
 && ln -s /actions-runner/externals/node24/bin/node /usr/local/bin/node \
 && ln -s /actions-runner/externals/node24/bin/npm /usr/local/bin/npm \
 && ln -s /actions-runner/externals/node24/bin/npx /usr/local/bin/npx
ENV FORCE_JAVASCRIPT_ACTIONS_TO_NODE24=true

# Go JIT-registration entrypoint — replaces myoung34's bash registration: sets up
# git/registry/Go-module auth, JIT-registers an ephemeral runner (replacing a
# stale same-named one on conflict), then execs the agent. dumb-init stays PID 1
# to reap job subprocess zombies + forward signals. See cmd/runner-entrypoint.
COPY --from=entrypoint-build /runner-entrypoint /usr/local/bin/runner-entrypoint

WORKDIR /actions-runner
ENTRYPOINT ["/usr/bin/dumb-init", "/usr/local/bin/runner-entrypoint"]
