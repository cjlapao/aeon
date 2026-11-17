#!/usr/bin/env bash
#
# B1: Aeon imports the framework. It never forks, vendors or reaches into it.
#
# The framework module is github.com/cjlapao/go-backend-scaffolding/framework.
# The scaffolding's own internal/ packages are NOT part of it, and Go already
# refuses those imports across a module boundary — so this guard is not about
# the compiler catching you.
#
# It is about the two things the compiler cannot see:
#
#   1. A COPY. Someone needing internal/api pastes it into aeon/internal/. It
#      compiles, every test passes, and the module now carries code with
#      someone else's provenance — which breaks the clean `git subtree split`
#      the eventual extraction depends on (P24), and quietly makes upstream
#      fixes to that code something nobody will ever apply.
#
#   2. A REPLACE. A `replace` directive pointing at a local checkout makes the
#      build work on one machine and fail in CI and for every other
#      contributor, with an error that names a path rather than the cause.
#
# Both are findings to REPORT, not things to tidy away: whoever did it had a
# reason, and the reason is usually a missing seam in the framework that should
# be raised there instead.
set -euo pipefail

cd "$(dirname "$0")/.."

SCAFFOLD='github.com/cjlapao/go-backend-scaffolding'
status=0

# 1. An import of the scaffolding that is not the framework module.
hits=$(grep -rn --include='*.go' "\"${SCAFFOLD}" cmd internal 2>/dev/null \
	| grep -v "\"${SCAFFOLD}/framework/" || true)
if [ -n "$hits" ]; then
	echo "FAIL: imports of the scaffolding outside its framework/ module:"
	echo "$hits" | sed 's/^/  /'
	echo ""
	echo "  Only ${SCAFFOLD}/framework/... crosses the module boundary."
	echo "  If you need something that does not, raise it as a framework request."
	status=1
fi

# 2. A replace directive — a build that works on one machine only.
if grep -qE '^[[:space:]]*replace[[:space:]]' go.mod 2>/dev/null; then
	echo "FAIL: go.mod contains a replace directive:"
	grep -nE '^[[:space:]]*replace[[:space:]]' go.mod | sed 's/^/  /'
	echo ""
	echo "  A replace makes the build local. Pin a published version instead (B2)."
	status=1
fi

# 3. A vendored or pasted copy of the framework.
if [ -d vendor ]; then
	echo "FAIL: vendor/ exists. Aeon pins a published version; it does not vendor (B1)."
	status=1
fi

[ $status -eq 0 ] && echo "  guard ok: no scaffolding internal/ imports, no replace, no vendor"
exit $status
