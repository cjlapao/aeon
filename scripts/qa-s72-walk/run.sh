#!/usr/bin/env bash
# S72 T4 — one-call runner for the browser walk. The harness sandbox gives each
# bash call its own /tmp, so playwright-core is installed --no-save at the
# repository root (node_modules is gitignored there) and consumed from it;
# nothing in package.json changes.
#   run.sh <scratch-dir> <rpc.sock> [--only <page-id>]
set -euo pipefail
SCRATCH="$1"; shift
SOCK="$1"; shift
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO="$(cd "$HERE/../../.." && pwd)"
mkdir -p "$SCRATCH/shots"
cd "$REPO"
[ -d node_modules/playwright-core ] || TMPDIR=/tmp npm_config_cache="$SCRATCH/npm-cache" npm install --no-save --no-audit --no-fund playwright-core >/dev/null
git diff --quiet package.json || { echo "run.sh: npm touched package.json — restore it"; exit 1; }
cd "$HERE"
TMPDIR=/tmp PW="$REPO/node_modules/playwright-core" \
  WALK_JSON="${WALK_JSON:-$SCRATCH/walk-results.json}" \
  node walk.mjs "$@" --keep-shots "$SCRATCH/shots"
