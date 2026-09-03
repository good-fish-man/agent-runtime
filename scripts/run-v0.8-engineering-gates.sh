#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WORKSPACE_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
CACHE_ROOT="${ATHENA_GATE_CACHE_ROOT:-${TMPDIR:-/tmp}/athena-v0.8-gates}"

run() {
  local name="$1"
  shift
  printf '\n[v0.8] %s\n' "$name"
  "$@"
}

mkdir -p "$CACHE_ROOT"

run "Provider contract, SDK, safety helpers, and buildable read-only example" \
  env GOCACHE="$CACHE_ROOT/protocol" GOWORK=off go -C "$WORKSPACE_ROOT/athena-protocol" test \
    ./protocol/plugin/v1 ./sdk/plugin ./sdk/safety ./examples/read-only-provider
run "protocol generation and compatibility checks" \
  env GOCACHE="$CACHE_ROOT/protocol" GOWORK=off make -C "$WORKSPACE_ROOT/athena-protocol" check
run "protocol static analysis" \
  env GOCACHE="$CACHE_ROOT/protocol" GOWORK=off go -C "$WORKSPACE_ROOT/athena-protocol" vet ./...

run "signed Provider loading, fail-closed grants, crash isolation, and invocation provenance" \
  env GOCACHE="$CACHE_ROOT/runtime" go -C "$WORKSPACE_ROOT/agent-runtime" test \
    ./internal/provider ./internal/capability ./internal/operations ./internal/readiness
run "Provider execution race checks" \
  env GOCACHE="$CACHE_ROOT/runtime-race" go -C "$WORKSPACE_ROOT/agent-runtime" test -race ./internal/provider
run "runtime regression tests" \
  env GOCACHE="$CACHE_ROOT/runtime" go -C "$WORKSPACE_ROOT/agent-runtime" test ./...
run "runtime static analysis" \
  env GOCACHE="$CACHE_ROOT/runtime" go -C "$WORKSPACE_ROOT/agent-runtime" vet ./...

run "Registry scan, trust, review, revoke, quarantine, and migration boundary" \
  env GOCACHE="$CACHE_ROOT/client" go -C "$WORKSPACE_ROOT/agent-runtime-client" test \
    ./application/service/pluginregistry ./api/http/handler/public/pluginregistry \
    ./api/http/router/public ./infra/repository/migration
run "Registry race checks" \
  env GOCACHE="$CACHE_ROOT/client-race" go -C "$WORKSPACE_ROOT/agent-runtime-client" test -race ./application/service/pluginregistry
run "runtime-client regression tests" \
  env GOCACHE="$CACHE_ROOT/client" go -C "$WORKSPACE_ROOT/agent-runtime-client" test ./...
run "runtime-client static analysis" \
  env GOCACHE="$CACHE_ROOT/client" go -C "$WORKSPACE_ROOT/agent-runtime-client" vet ./...

run "Launcher Registry and trust bootstrap" \
  env GOCACHE="$CACHE_ROOT/launcher" go -C "$WORKSPACE_ROOT/athena-launcher" test ./internal/launcher/deployment
run "Launcher regression tests" \
  env GOCACHE="$CACHE_ROOT/launcher" go -C "$WORKSPACE_ROOT/athena-launcher" test ./...
run "Launcher static analysis" \
  env GOCACHE="$CACHE_ROOT/launcher" go -C "$WORKSPACE_ROOT/athena-launcher" vet ./...

run "frontend protocol synchronization" npm --prefix "$WORKSPACE_ROOT/frontend/agent-ui" run protocol:check
run "frontend type check" npm --prefix "$WORKSPACE_ROOT/frontend/agent-ui" run lint
run "frontend production build" npm --prefix "$WORKSPACE_ROOT/frontend/agent-ui" run build

printf '\n[v0.8] ENGINEERING_VERIFIED\n'
printf '[v0.8] Public Registry publication, third-party key custody, and production sandbox certification remain release-environment gates.\n'
