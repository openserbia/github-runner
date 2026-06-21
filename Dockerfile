# SPDX-FileCopyrightText: 2026 OpenSerbia
# SPDX-License-Identifier: MIT
# github-runner — myoung34/github-runner (Ubuntu Noble) + the openserbia CI
# toolchain (Go, Node LTS, go-task, devbox, docker compose) and a git-auth
# entrypoint wrapper. Published multi-arch (amd64 for the AX41 pool, arm64 for
# the rpi pool) so ONE weekly-rebuilt image serves the whole self-hosted fleet.
#
# :latest is deliberate — the weekly build pulls a fresh myoung34 base + apt
# upgrade, which is the security-patch channel; pinning the base by digest here
# would freeze out those rebuilds. Dependabot bumps the FROM via PR instead.
# hadolint ignore=DL3007
FROM myoung34/github-runner:ubuntu-noble

# Pinned toolchain versions — bumped deliberately (Dependabot / PR), so a
# rebuild against an unchanged base is reproducible and the last Trivy scan
# still applies.
ARG GO_VERSION=1.26.4
ARG NODE_MAJOR=24
# go-task — pinned release (taskfile.dev/install.sh -b ... accepts a version).
ARG TASK_VERSION=v3.51.1
# Docker Compose CLI plugin — pinned (formerly tracked :latest).
ARG COMPOSE_VERSION=v5.1.4

ENV PATH="/usr/local/go/bin:${PATH}"

# OS security patches — the whole point of the weekly rebuild. myoung34's base
# is only rebuilt weekly upstream; a full upgrade here pulls any Ubuntu fixes
# published since, clearing the *fixable* OS-package CVEs the scan flags.
# (Unfixable kernel-header CVEs in linux-libc-dev remain — they don't apply to a
# container that uses the HOST kernel; the scan gate uses --ignore-unfixed so
# they never fail the build.)
RUN apt-get update \
 && DEBIAN_FRONTEND=noninteractive apt-get -y upgrade \
 && rm -rf /var/lib/apt/lists/*

# Go from the official tarball (arch-aware: amd64 / arm64).
RUN ARCH="$(dpkg --print-architecture)" \
 && curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-${ARCH}.tar.gz" | tar -C /usr/local -xz

# Node.js LTS (nodesource).
# hadolint ignore=DL3008,DL3009
RUN curl -fsSL "https://deb.nodesource.com/setup_${NODE_MAJOR}.x" | bash - \
 && apt-get install -y --no-install-recommends nodejs \
 && rm -rf /var/lib/apt/lists/*

# go-task (pinned).
RUN sh -c "$(curl --location https://taskfile.dev/install.sh)" -- -d -b /usr/local/bin "${TASK_VERSION}"

# devbox.
RUN curl -fsSL https://get.jetify.com/devbox | bash -s -- -f

# docker compose CLI plugin (pinned, arch-aware: x86_64 / aarch64).
RUN ARCH="$(uname -m)" \
 && mkdir -p /usr/local/lib/docker/cli-plugins \
 && curl -fsSL "https://github.com/docker/compose/releases/download/${COMPOSE_VERSION}/docker-compose-linux-${ARCH}" \
    -o /usr/local/lib/docker/cli-plugins/docker-compose \
 && chmod +x /usr/local/lib/docker/cli-plugins/docker-compose

# Recompile git-lfs against our Go toolchain. The base ships git-lfs 3.7.1 built
# with an older Go whose stdlib carries CVE-2025-68121 — the lone *fixable*
# CRITICAL the scan flags. git-lfs has no newer release, so we rebuild the same
# 3.7.1 tag from source with Go ${GO_VERSION} (patched stdlib) and overwrite the
# base binary IN PLACE: Trivy scans the file on disk, so shadowing via PATH is
# not enough — the vulnerable file itself must be replaced. `go install` on
# v3.7.1 reproduces the correct version string (verified:
# "git-lfs/3.7.1 (... go 1.26.4)"). If this ever fails to build, the scan gate
# catches the unpatched binary and the build stays red rather than shipping it.
RUN go install github.com/git-lfs/git-lfs/v3@v3.7.1 \
 && NEW="$(go env GOPATH)/bin/git-lfs" \
 && for p in "$(command -v git-lfs || true)" /usr/bin/git-lfs /usr/local/bin/git-lfs; do \
        if [ -n "$p" ] && [ -e "$p" ]; then install -m0755 "$NEW" "$p"; fi; \
    done \
 && rm -rf "$(go env GOPATH)/bin/git-lfs" "$(go env GOPATH)/pkg" "$(go env GOCACHE)"

# git-auth entrypoint wrapper — configures git URL rewrites + Go private-module
# settings from GITHUB_PAT at runtime, then hands off to the base entrypoint.
COPY entrypoint-wrapper.sh /entrypoint-wrapper.sh
RUN chmod +x /entrypoint-wrapper.sh

ENTRYPOINT ["/entrypoint-wrapper.sh"]
CMD ["./bin/Runner.Listener", "run", "--startuptype", "service"]
