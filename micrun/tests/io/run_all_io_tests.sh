#!/bin/bash
# Adaptive one-click entry for MicRun IO tests.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

exec "$SCRIPT_DIR/test_suite.sh" "$@"
