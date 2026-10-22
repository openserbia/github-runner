#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 OpenSerbia
# SPDX-License-Identifier: MIT
# Smoke test for github-runner: the real CMD launches the Actions runner, which
# needs registration creds we don't have in CI. So instead of registering, we
# override the entrypoint and assert the runner binary + the CI toolchain we add
# are present and sane — and, critically, that git-lfs was recompiled against a
# patched Go (the CVE-2025-68121 fix must not silently regress).
# Usage: test/smoke.sh <image-ref>
set -euo pipefail

IMAGE="${1:?usage: smoke.sh <image-ref>}"

# Run a one-shot command inside the image with the entrypoint bypassed (the
# wrapper would otherwise try to configure auth and exec the runner).
run() { docker run --rm --memory 1g --entrypoint bash "$IMAGE" -c "$1"; }

echo "runner binary..."
run './bin/Runner.Listener --version'

# node/npm/npx on PATH are links to the runner's node24 (the official Node
# build the Dockerfile swaps in). It runs every JS action: node20 is pruned, and
# FORCE_JAVASCRIPT_ACTIONS_TO_NODE24 sends node20 actions to it.
echo "node...";                run 'node --version && npm --version && test ! -e /actions-runner/externals/node20'
# Floors, not exact pins, so a NODE_VERSION bump needs no edit here: Node 24 at
# or past the 24.22.1 security release, and npm's bundled tar at or past 7.5.22
# (CVE-2026-59873). Raise them when a newer fix must not regress.
echo "node24 + npm tar at or past their security floors..."
# shellcheck disable=SC2016  # $path must expand in the CONTAINER's shell, not
# here — run() forwards "$1" verbatim to `bash -c` inside the image.
run '
  path=/actions-runner/externals/node24/lib/node_modules/npm/node_modules/tar
  node -e "
    const ge = (v, min) => { const a = v.split(\".\").map(Number), b = min.split(\".\").map(Number);
      for (let i = 0; i < 3; i++) if (a[i] !== b[i]) return a[i] > b[i]; return true; };
    const node = process.versions.node, tar = require(\"$path/package.json\").version;
    console.log(\"node\", node, \"tar\", tar);
    if (!node.startsWith(\"24.\") || !ge(node, \"24.22.1\") || !ge(tar, \"7.5.22\")) process.exit(1);
    if (typeof require(\"$path\").x !== \"function\") process.exit(1);
  "
'
echo "libssl.so.3 for .NET..."; run 'test -e /usr/lib/libssl.so.3'
echo "docker buildx plugin...";  run 'docker buildx version'    # workflows build images via BuildKit
echo "gh CLI...";                run 'gh --version'
# actions/cache shells out to `tar --posix` + zstd; busybox tar breaks it.
echo "GNU tar (not busybox)...";  run 'tar --version | head -1 | grep -q "GNU tar" && tar --version | head -1'
echo "zstd...";                   run 'zstd --version'
# The Nix installer runs `mount -f` when /nix is a volume mount point.
echo "mount (Nix on a /nix volume)..."; run 'mount --version'

echo "Go registration entrypoint present + runs (fails fast without config)..."
run 'test -x /usr/local/bin/runner-entrypoint'
# With no registration env it must exit non-zero with a clear config error,
# proving the static binary actually executes in the image.
out=$(docker run --rm --memory 1g --entrypoint /usr/local/bin/runner-entrypoint "$IMAGE" 2>&1 || true)
echo "  $out"
case "$out" in
  *"ORG_NAME is required"*) ;;
  *) echo "FAIL: entrypoint did not report expected config error; got: $out"; exit 1 ;;
esac

echo "git-lfs must be built against patched Go (>=1.26 — Wolfi apk; CVE-2025-68121 guard)..."
glfs_line=$(run 'git-lfs version')
echo "  $glfs_line"
glfs_go=$(echo "$glfs_line" | grep -oE 'go [0-9]+\.[0-9]+\.[0-9]+' | awk '{print $2}')
if [ -z "$glfs_go" ]; then
  echo "FAIL: could not parse git-lfs Go version from: $glfs_line"; exit 1
fi
major=${glfs_go%%.*}; rest=${glfs_go#*.}; minor=${rest%%.*}
# CVE-2025-68121 is fixed in Go 1.25.7 / 1.26.0+. We build with 1.26.x; require
# >= 1.26 (rejects the base's vulnerable 1.25.3 if the recompile ever no-ops).
if [ "$major" -lt 1 ] || { [ "$major" -eq 1 ] && [ "$minor" -lt 26 ]; }; then
  echo "FAIL: git-lfs built with go $glfs_go (<1.26) — CVE-2025-68121 fix regressed"; exit 1
fi

echo "SMOKE OK: $IMAGE (git-lfs go=$glfs_go)"
