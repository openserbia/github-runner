# SPDX-FileCopyrightText: 2026 OpenSerbia
# SPDX-License-Identifier: MIT
# github-runner — a GitHub Actions self-hosted runner on Chainguard Wolfi (glibc,
# daily-patched, minimal) + the openserbia CI toolchain (Go, Node LTS, go-task,
# devbox, docker compose) and a Go JIT-registration entrypoint
# (cmd/runner-entrypoint). Published multi-arch (amd64 + arm64).
#
# The actions/runner agent bundles its OWN trimmed .NET runtime + node20/node24;
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
FROM golang:1.26 AS entrypoint-build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go vet ./... \
 && CGO_ENABLED=0 go test ./... \
 && CGO_ENABLED=0 go build -trimpath -tags netgo,osusergo -ldflags="-s -w" \
    -o /runner-entrypoint ./cmd/runner-entrypoint

# ---- runtime ---------------------------------------------------------------
# hadolint ignore=DL3007
FROM cgr.dev/chainguard/wolfi-base:latest

# Pinned (deliberate-bump) versions for the bits NOT delivered via Wolfi's repo.
ARG RUNNER_VERSION=2.335.1
ARG NODE_MAJOR=24
ARG TASK_VERSION=v3.51.1
ARG COMPOSE_VERSION=v5.1.4

# Runtime deps, installed first with the default /bin/sh (wolfi-base has no bash):
#  - native libs the bundled .NET runner loads (icu/krb5/openssl/zlib/lttng-ust/
#    libstdc++/libgcc)
#  - tools workflows use (go-1.26, git, git-lfs, docker-cli, nodejs-${NODE_MAJOR})
#  - bash/curl/xz/dumb-init/ca-certs for the entrypoint, downloads, devbox/Nix, TLS
# Wolfi's rolling repo IS the patch-delivery channel, so versions are unpinned.
# hadolint ignore=DL3018
# gnutar (GNU tar, overrides busybox tar) + zstd: actions/cache invokes
# `tar --posix` / zstd, which busybox tar can't do — without these, cache
# save/restore fails on every job.
RUN apk add --no-cache \
      bash curl xz git jq yq gh gnutar zstd dumb-init ca-certificates-bundle \
      icu-libs krb5-libs openssl openssl-config zlib libstdc++ libgcc lttng-ust \
      go-1.26 git-lfs docker-cli docker-cli-buildx "nodejs-${NODE_MAJOR}"

# bash now exists → use it with pipefail so the curl|… downloads below fail fast.
SHELL ["/bin/bash", "-o", "pipefail", "-c"]

# go-task (pinned).
RUN sh -c "$(curl --location https://taskfile.dev/install.sh)" -- -d -b /usr/local/bin "${TASK_VERSION}"

# docker compose CLI plugin (pinned, arch-aware: x86_64 / aarch64).
RUN ARCH="$(uname -m)" \
 && mkdir -p /usr/local/lib/docker/cli-plugins \
 && curl -fsSL "https://github.com/docker/compose/releases/download/${COMPOSE_VERSION}/docker-compose-linux-${ARCH}" \
    -o /usr/local/lib/docker/cli-plugins/docker-compose \
 && chmod +x /usr/local/lib/docker/cli-plugins/docker-compose

# devbox (binary only; Nix provisions on first `devbox run` inside a workflow).
RUN curl -fsSL https://get.jetify.com/devbox | bash -s -- -f

# actions/runner agent (arch-aware: x64 / arm64). Bundles its own .NET + nodes.
RUN case "$(uname -m)" in \
      x86_64) rarch=x64 ;; \
      aarch64) rarch=arm64 ;; \
      *) echo "unsupported arch: $(uname -m)" >&2; exit 1 ;; \
    esac \
 && mkdir -p /actions-runner \
 && curl -fsSL "https://github.com/actions/runner/releases/download/v${RUNNER_VERSION}/actions-runner-linux-${rarch}-${RUNNER_VERSION}.tar.gz" \
    | tar -C /actions-runner -xz

# Go JIT-registration entrypoint — replaces myoung34's bash registration: sets up
# git/registry/Go-module auth, JIT-registers an ephemeral runner (replacing a
# stale same-named one on conflict), then execs the agent. dumb-init stays PID 1
# to reap job subprocess zombies + forward signals. See cmd/runner-entrypoint.
COPY --from=entrypoint-build /runner-entrypoint /usr/local/bin/runner-entrypoint

WORKDIR /actions-runner
ENTRYPOINT ["/usr/bin/dumb-init", "/usr/local/bin/runner-entrypoint"]
