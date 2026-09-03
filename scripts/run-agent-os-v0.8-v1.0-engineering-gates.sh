#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

for version in 0.8 0.9 1.0; do
  printf '\n[agent-os] running v%s engineering gates\n' "$version"
  "$SCRIPT_DIR/run-v${version}-engineering-gates.sh"
done

printf '\n[agent-os] v0.8-v1.0 ENGINEERING_VERIFIED\n'
printf '[agent-os] RELEASE_STATUS=EXTERNAL_REQUIRED; local engineering verification is not a GA declaration.\n'
printf '[agent-os] Publish the coordinated athena-protocol module before standalone repository CI/release builds.\n'
