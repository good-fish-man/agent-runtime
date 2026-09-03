#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WORKSPACE_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
CACHE_ROOT="${ATHENA_GATE_CACHE_ROOT:-${TMPDIR:-/tmp}/athena-v0.9-gates}"

run() {
  local name="$1"
  shift
  printf '\n[v0.9] %s\n' "$name"
  "$@"
}

mkdir -p "$CACHE_ROOT"

run "operations, recovery, lease, health, and SLO protocol" \
  env GOCACHE="$CACHE_ROOT/protocol" GOWORK=off go -C "$WORKSPACE_ROOT/athena-protocol" test ./protocol/operations/v1
run "protocol generation and compatibility checks" \
  env GOCACHE="$CACHE_ROOT/protocol" GOWORK=off make -C "$WORKSPACE_ROOT/athena-protocol" check
run "protocol static analysis" \
  env GOCACHE="$CACHE_ROOT/protocol" GOWORK=off go -C "$WORKSPACE_ROOT/athena-protocol" vet ./...

run "bounded admission, deadlines, drain, health, and SLO" \
  env GOCACHE="$CACHE_ROOT/runtime" go -C "$WORKSPACE_ROOT/agent-runtime" test \
    ./internal/operations ./internal/server ./internal/readiness
run "runtime operations race checks" \
  env GOCACHE="$CACHE_ROOT/runtime-race" go -C "$WORKSPACE_ROOT/agent-runtime" test -race ./internal/operations ./internal/server
run "runtime regression tests" \
  env GOCACHE="$CACHE_ROOT/runtime" go -C "$WORKSPACE_ROOT/agent-runtime" test ./...
run "runtime static analysis" \
  env GOCACHE="$CACHE_ROOT/runtime" go -C "$WORKSPACE_ROOT/agent-runtime" vet ./...

run "encrypted backup, authenticated inventory, restore, device lease, fencing, and migrations" \
  env GOCACHE="$CACHE_ROOT/client" go -C "$WORKSPACE_ROOT/agent-runtime-client" test \
    ./application/service/operations ./application/service/control \
    ./api/http/handler/public/operations ./api/http/router/public \
    ./infra/repository/repo/operations ./infra/repository/repo/control ./infra/repository/migration
run "control-plane recovery and lease race checks" \
  env GOCACHE="$CACHE_ROOT/client-race" go -C "$WORKSPACE_ROOT/agent-runtime-client" test -race \
    ./application/service/operations ./application/service/control
run "runtime-client regression tests" \
  env GOCACHE="$CACHE_ROOT/client" go -C "$WORKSPACE_ROOT/agent-runtime-client" test ./...
run "runtime-client static analysis" \
  env GOCACHE="$CACHE_ROOT/client" go -C "$WORKSPACE_ROOT/agent-runtime-client" vet ./...

run "signed manifest, artifact integrity, managed recovery, update, and rollback" \
  env GOCACHE="$CACHE_ROOT/launcher" go -C "$WORKSPACE_ROOT/athena-launcher" test ./internal/release ./internal/launcher/deployment
run "Launcher release and recovery race checks" \
  env GOCACHE="$CACHE_ROOT/launcher-race" go -C "$WORKSPACE_ROOT/athena-launcher" test -race ./internal/release ./internal/launcher/deployment
run "Launcher regression tests" \
  env GOCACHE="$CACHE_ROOT/launcher" go -C "$WORKSPACE_ROOT/athena-launcher" test ./...
run "Launcher static analysis" \
  env GOCACHE="$CACHE_ROOT/launcher" go -C "$WORKSPACE_ROOT/athena-launcher" vet ./...

run "frontend protocol synchronization" npm --prefix "$WORKSPACE_ROOT/frontend/agent-ui" run protocol:check
run "frontend type check" npm --prefix "$WORKSPACE_ROOT/frontend/agent-ui" run lint
run "frontend production build" npm --prefix "$WORKSPACE_ROOT/frontend/agent-ui" run build

printf '\n[v0.9] ENGINEERING_VERIFIED\n'
printf '[v0.9] GA still requires signed/notarized platform packages, a real DR drill, security review, fault injection, and 24/72-hour SLO evidence.\n'
