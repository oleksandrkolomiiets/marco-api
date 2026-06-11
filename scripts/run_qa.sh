#!/usr/bin/env bash
# Thin wrapper around `go run ./cmd/qa`. MARCO_DEV_MODE is the safety gate —
# cmd/qa refuses to run without it, so this script is the canonical entry point
# and protects against accidentally pointing the harness at a non-local DB.
set -euo pipefail

export MARCO_DEV_MODE=true
exec go run ./cmd/qa "$@"
