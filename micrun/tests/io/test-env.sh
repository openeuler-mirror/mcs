#!/bin/bash
# MicRun IO Test Environment Configuration
# Source this file before running tests: source test-env.sh

_MICRUN_IO_ENV_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${_MICRUN_IO_ENV_DIR}/../common/env.sh"

print_io_test_env
