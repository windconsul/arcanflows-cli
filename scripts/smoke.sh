#!/usr/bin/env bash
# PROPOSAL-010 Phase 4.4 — offline smoke suite for the ArcanFlows CLI.
#
# Builds the binary, runs the full unit-test suite, and exercises the
# offline-safe command surface (no network / no login needed) so obvious
# breakage is caught before release. The network paths (login → chat → repo
# analyze) require a live server and are covered by manual/CI E2E instead.
#
# Usage: cli/scripts/smoke.sh
set -uo pipefail
cd "$(dirname "$0")/.."

PASS=0 FAIL=0
ok()  { echo "  ✅ $1"; PASS=$((PASS+1)); }
bad() { echo "  ❌ $1"; FAIL=$((FAIL+1)); }
run() { # run <desc> <cmd...>
  local desc="$1"; shift
  if "$@" >/dev/null 2>&1; then ok "$desc"; else bad "$desc"; fi
}

echo "== build & vet =="
run "go build" go build ./...
run "go vet" go vet ./...

echo "== unit tests =="
if go test ./... >/tmp/af-test.log 2>&1; then ok "go test ./..."; else bad "go test ./... (see /tmp/af-test.log)"; fi

echo "== binary surface (offline) =="
BIN=/tmp/arcanflows-smoke
go build -o "$BIN" . || { bad "binary build"; echo "$PASS passed / $((FAIL+1)) failed"; exit 1; }
run "--help"                "$BIN" --help
run "--version/version"     "$BIN" version
run "chat --help"           "$BIN" chat --help
run "chat --help shows --allow-outside"  bash -c "$BIN chat --help 2>&1 | grep -q allow-outside"
run "repo --help"           "$BIN" repo --help
run "hooks --help"          "$BIN" hooks --help
run "config --help"         "$BIN" config --help
run "completion bash"       "$BIN" completion bash
run "unknown cmd errors"    bash -c "! $BIN definitely-not-a-command >/dev/null 2>&1"

echo
echo "================  $PASS passed / $FAIL failed  ================"
[ "$FAIL" -eq 0 ]
