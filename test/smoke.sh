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

echo "go toolchain...";        run 'go version'
echo "node...";                run 'node --version'
echo "go-task...";             run 'task --version'
echo "docker compose plugin..."; run 'docker compose version'   # --version needs no daemon

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
