#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cache="${GOCACHE:-/private/tmp/athena-v03-w0-audit-cache}"

"$root/scripts/verify-v0.2-spans.sh" "$root/testdata/v0.3-w0/spans/complete.txt" arc-v03-w0-fixture >/dev/null
if "$root/scripts/verify-v0.2-spans.sh" "$root/testdata/v0.3-w0/spans/incomplete.txt" arc-v03-w0-incomplete >/dev/null 2>&1; then
	echo "incomplete span fixture was incorrectly accepted" >&2
	exit 1
fi

(
	cd "$root"
	GOCACHE="$cache" go run ./cmd/v03-evidence-audit --format json testdata/v0.3-w0/credentials/clean.txt >/dev/null
)
if (
	cd "$root"
	GOCACHE="$cache" go run ./cmd/v03-evidence-audit --format json testdata/v0.3-w0/credentials/leak.txt >/dev/null 2>&1
); then
	echo "plaintext credential fixture was incorrectly accepted" >&2
	exit 1
fi

echo "V3-W0 span and credential auditors passed positive and negative fixtures"
