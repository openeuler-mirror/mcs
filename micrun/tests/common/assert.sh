#!/bin/bash

if [ -n "${MICRUN_TEST_ASSERT_SH_LOADED:-}" ]; then
    return 0
fi
MICRUN_TEST_ASSERT_SH_LOADED=1

readonly COLOR_RED='\033[0;31m'
readonly COLOR_GREEN='\033[0;32m'
readonly COLOR_YELLOW='\033[0;33m'
readonly COLOR_BLUE='\033[0;34m'
readonly COLOR_NC='\033[0m'

readonly PASS="${COLOR_GREEN}✓ PASS${COLOR_NC}"
readonly FAIL="${COLOR_RED}✗ FAIL${COLOR_NC}"
readonly SKIP="${COLOR_YELLOW}○ SKIP${COLOR_NC}"
readonly INFO="${COLOR_BLUE}[INFO]${COLOR_NC}"

log_info() {
    echo -e "${INFO} $1"
}

log_test() {
    echo -e "\n${INFO}▶ Testing:${COLOR_NC} $1"
}

log_success() {
    echo -e "${COLOR_GREEN}✓${COLOR_NC} $1"
}

log_error() {
    echo -e "${COLOR_RED}✗${COLOR_NC} $1"
}

log_warn() {
    echo -e "${COLOR_YELLOW}⚠${COLOR_NC} $1"
}

assert_equals() {
    local expected="$1"
    local actual="$2"
    local message="${3:-Assertion failed}"

    if [ "$expected" != "$actual" ]; then
        log_error "$message"
        log_error "  Expected: $expected"
        log_error "  Actual: $actual"
        return 1
    fi
    return 0
}

assert_contains() {
    local haystack="$1"
    local needle="$2"
    local message="${3:-Assertion failed}"

    if printf '%s\n' "$haystack" | grep -q "$needle"; then
        return 0
    fi

    log_error "$message"
    return 1
}

assert_not_empty() {
    local value="$1"
    local message="${2:-Assertion failed: value is empty}"

    if [ -z "$value" ]; then
        log_error "$message"
        return 1
    fi
    return 0
}

assert_success() {
    local exit_code="$1"
    local message="${2:-Command failed}"

    if [ "$exit_code" -ne 0 ]; then
        log_error "$message (exit code: $exit_code)"
        return 1
    fi
    return 0
}

timer_start() {
    date +%s
}

timer_end() {
    local start_time="$1"
    local end_time

    end_time="$(date +%s)"
    echo $((end_time - start_time))
}

wait_until() {
    local command="$1"
    local retries="${2:-60}"
    local sleep_seconds="${3:-2}"
    local i

    for i in $(seq 1 "$retries"); do
        if bash -lc "$command" >/dev/null 2>&1; then
            return 0
        fi
        sleep "$sleep_seconds"
    done

    return 1
}
