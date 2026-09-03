#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

for version in 0.4 0.5 0.6 0.7; do
  printf '\n[agent-os] running v%s engineering gates\n' "$version"
  "$SCRIPT_DIR/run-v${version}-engineering-gates.sh"
done

printf '\n[agent-os] v0.4-v0.7 ENGINEERING_VERIFIED\n'
printf '[agent-os] External release evidence and GA gates remain separate and open.\n'
printf '[agent-os] Standalone repository CI requires publishing the coordinated athena-protocol v0.2.0 module first.\n'
