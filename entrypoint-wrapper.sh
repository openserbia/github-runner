#!/bin/bash
# SPDX-FileCopyrightText: 2026 OpenSerbia
# SPDX-License-Identifier: MIT
# Configures git/registry auth and Go private-module settings from runtime env,
# then hands off to the base myoung34 entrypoint (which configures + runs the
# Actions runner). All secrets come from env at RUNTIME — nothing is baked into
# the image.
set -euo pipefail

# Copy host registry auth into writable ~/.docker/ (host mount is read-only).
if [ -f /docker-config/config.json ]; then
  mkdir -p "$HOME/.docker"
  cp /docker-config/config.json "$HOME/.docker/config.json"
fi

# Configure git URL rewrites for private-repo access, and write the PAT to a
# file for Docker build secrets (Taskfiles use --secret id=...,src=$GITHUB_TOKEN_FILE).
if [ -n "${GITHUB_PAT:-}" ]; then
  git config --global url."https://oauth2:${GITHUB_PAT}@github.com/".insteadOf "https://github.com/"
  git config --global url."https://oauth2:${GITHUB_PAT}@github.com/".insteadOf "git@github.com:"
  git config --global url."https://oauth2:${GITHUB_PAT}@github.com/".insteadOf "ssh://git@github.com/"

  echo -n "$GITHUB_PAT" > /tmp/.github-pat
  chmod 600 /tmp/.github-pat
  export GITHUB_TOKEN_FILE=/tmp/.github-pat
fi

# Go private-module settings.
export GOPRIVATE="${GOPRIVATE:-github.com/openserbia/*}"
export GONOSUMCHECK="${GONOSUMCHECK:-github.com/openserbia/*}"

exec /entrypoint.sh "$@"
