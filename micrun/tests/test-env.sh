#!/bin/bash
# MicRun 测试环境配置
# 使用方式: source tests/test-env.sh

_MICRUN_TEST_ENV_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${_MICRUN_TEST_ENV_DIR}/common/env.sh"

if [ -z "${MICRUN_TEST_ENV_PRINTED:-}" ]; then
    export MICRUN_TEST_ENV_PRINTED=1
    print_common_test_env
fi
