#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WORKSPACE_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
CACHE_ROOT="${ATHENA_GATE_CACHE_ROOT:-${TMPDIR:-/tmp}/athena-v0.6-gates}"

run() {
  local name="$1"
  shift
  printf '\n[v0.6] %s\n' "$name"
  "$@"
}

mkdir -p "$CACHE_ROOT"

run "knowledge protocol contract and validation" \
  env GOCACHE="$CACHE_ROOT/protocol" GOWORK=off go -C "$WORKSPACE_ROOT/athena-protocol" test ./protocol/knowledge/v1
run "protocol contract, schema, fixture, and freeze checks" \
  env GOCACHE="$CACHE_ROOT/protocol" GOWORK=off make -C "$WORKSPACE_ROOT/athena-protocol" check
run "protocol static analysis" \
  env GOCACHE="$CACHE_ROOT/protocol" GOWORK=off go -C "$WORKSPACE_ROOT/athena-protocol" vet ./...

run "knowledge evidence, retrieval, ontology, runtime binding, and migration gates" \
  env GOCACHE="$CACHE_ROOT/client" go -C "$WORKSPACE_ROOT/agent-runtime-client" test \
    ./application/service/knowledge ./application/service/runtime ./infra/repository/migration ./infra/repository/repo/knowledge
run "runtime-client tests" \
  env GOCACHE="$CACHE_ROOT/client" go -C "$WORKSPACE_ROOT/agent-runtime-client" test ./...
run "runtime-client static analysis" \
  env GOCACHE="$CACHE_ROOT/client" go -C "$WORKSPACE_ROOT/agent-runtime-client" vet ./...

run "runtime tests" \
  env GOCACHE="$CACHE_ROOT/runtime" go -C "$WORKSPACE_ROOT/agent-runtime" test ./...
run "runtime static analysis" \
  env GOCACHE="$CACHE_ROOT/runtime" go -C "$WORKSPACE_ROOT/agent-runtime" vet ./...

run "launcher tests" \
  env GOCACHE="$CACHE_ROOT/launcher" go -C "$WORKSPACE_ROOT/athena-launcher" test ./...
run "launcher static analysis" \
  env GOCACHE="$CACHE_ROOT/launcher" go -C "$WORKSPACE_ROOT/athena-launcher" vet ./...

run "frontend type check" npm --prefix "$WORKSPACE_ROOT/frontend/agent-ui" run lint
run "frontend production build" npm --prefix "$WORKSPACE_ROOT/frontend/agent-ui" run build

printf '\n[v0.6] ENGINEERING_VERIFIED\n'
printf '[v0.6] This result does not close the external v0.3 release-evidence gates.\n'
