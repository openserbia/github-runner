#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 OpenSerbia
# SPDX-License-Identifier: MIT
# Bump the Dockerfile ARG pins that Dependabot can't parse to their latest
# upstream releases, digests included:
#   RUNNER_VERSION + RUNNER_SHA256_{X64,ARM64}  actions/runner (release notes)
#   NODE_VERSION                                newest Node 24.x (the runner has
#                                               no newer JS-action handler)
#   DEVBOX_VERSION + DEVBOX_COMMIT              jetify-com/devbox (tag -> commit)
# Edits ./Dockerfile in place and prints a markdown summary of what moved (empty
# when everything is current). Needs: gh (authenticated), curl, jq, sed.
# Usage: .github/scripts/bump-pins.sh
set -euo pipefail

dockerfile=Dockerfile
summary=""

arg() { sed -nE "s/^ARG $1=(.*)$/\1/p" "$dockerfile"; }

set_arg() {
  local name=$1 value=$2
  [[ $value =~ ^[A-Za-z0-9._-]+$ ]] || { echo "refusing odd value for $name: $value" >&2; exit 1; }
  sed -i -E "s|^ARG $name=.*$|ARG $name=$value|" "$dockerfile"
}

note() { summary+="- $1"$'\n'; }

# --- actions/runner -----------------------------------------------------------
cur=$(arg RUNNER_VERSION)
tag=$(gh release view -R actions/runner --json tagName -q .tagName)
new=${tag#v}
if [[ $new != "$cur" ]]; then
  body=$(gh release view -R actions/runner "$tag" --json body -q .body)
  sha() { grep -oE "<!-- BEGIN SHA linux-$1 -->[0-9a-f]{64}" <<<"$body" | grep -oE '[0-9a-f]{64}$'; }
  x64=$(sha x64) arm64=$(sha arm64)
  [[ -n $x64 && -n $arm64 ]] || { echo "no linux SHA-256 in runner $tag notes" >&2; exit 1; }
  set_arg RUNNER_VERSION "$new"
  set_arg RUNNER_SHA256_X64 "$x64"
  set_arg RUNNER_SHA256_ARM64 "$arm64"
  note "actions/runner $cur → $new ([notes](https://github.com/actions/runner/releases/tag/$tag))"
fi

# --- Node 24 ------------------------------------------------------------------
cur=$(arg NODE_VERSION)
new=$(curl -fsSL https://nodejs.org/dist/index.json \
  | jq -r '[.[] | select(.version | startswith("v24."))][0].version | ltrimstr("v")')
if [[ -n $new && $new != "$cur" ]]; then
  sec=$(curl -fsSL https://nodejs.org/dist/index.json \
    | jq -r --arg v "v$new" '.[] | select(.version == $v) | .security')
  set_arg NODE_VERSION "$new"
  note "Node $cur → $new$([[ $sec == true ]] && echo ' (**security release**)')"
fi

# --- devbox -------------------------------------------------------------------
cur=$(arg DEVBOX_VERSION)
new=$(gh release view -R jetify-com/devbox --json tagName -q .tagName)
if [[ $new != "$cur" ]]; then
  # Annotated tags: the ^{} line is the commit the tag points at.
  commit=$(git ls-remote https://github.com/jetify-com/devbox "refs/tags/$new^{}" | cut -f1)
  [[ -n $commit ]] || commit=$(git ls-remote https://github.com/jetify-com/devbox "refs/tags/$new" | cut -f1)
  [[ $commit =~ ^[0-9a-f]{40}$ ]] || { echo "cannot resolve devbox $new to a commit" >&2; exit 1; }
  set_arg DEVBOX_VERSION "$new"
  set_arg DEVBOX_COMMIT "$commit"
  note "devbox $cur → $new; review DEVBOX_DEP_OVERRIDES, which may now be redundant ([notes](https://github.com/jetify-com/devbox/releases/tag/$new))"
fi

printf '%s' "$summary"
