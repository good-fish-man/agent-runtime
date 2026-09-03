#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WORKSPACE_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
CACHE_ROOT="${ATHENA_GATE_CACHE_ROOT:-${TMPDIR:-/tmp}/athena-v0.7-gates}"

run() {
  local name="$1"
  shift
  printf '\n[v0.7] %s\n' "$name"
  "$@"
}

mkdir -p "$CACHE_ROOT"

run "orchestration protocol, JSON Schema, routing, budgets, and checkpoint contract" \
  env GOCACHE="$CACHE_ROOT/protocol" GOWORK=off go -C "$WORKSPACE_ROOT/athena-protocol" test ./protocol/orchestration/v2
run "protocol contract, generated fixtures, and freeze checks" \
  env GOCACHE="$CACHE_ROOT/protocol" GOWORK=off make -C "$WORKSPACE_ROOT/athena-protocol" check
run "protocol static analysis" \
  env GOCACHE="$CACHE_ROOT/protocol" GOWORK=off go -C "$WORKSPACE_ROOT/athena-protocol" vet ./...

run "durable goals, bounded supervisor, schedules, HTTP control boundary, and migrations" \
  env GOCACHE="$CACHE_ROOT/client" go -C "$WORKSPACE_ROOT/agent-runtime-client" test \
    ./application/service/orchestration \
    ./application/service/scheduledtask \
    ./api/http/handler/public/orchestration \
    ./api/http/router/public \
    ./infra/repository/migration \
    ./infra/repository/repo/orchestration
run "orchestration and scheduler race checks" \
  env GOCACHE="$CACHE_ROOT/client-race" go -C "$WORKSPACE_ROOT/agent-runtime-client" test -race \
    ./application/service/orchestration ./application/service/scheduledtask
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
run "destructive rollback artifact is present" test -f "$WORKSPACE_ROOT/agent-runtime-client/migrations/v0.7-orchestration-rollback.sql"

printf '\n[v0.7] ENGINEERING_VERIFIED\n'
printf '[v0.7] This result uses deterministic Runtime/device fixtures and does not close external v0.3 release-evidence gates.\n'
