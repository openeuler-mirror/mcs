#!/bin/bash
# MicRun IO Test Environment Configuration
# Source this file before running tests: source test-env.sh

export REMOTE_HOST="${REMOTE_HOST:-root@192.168.7.2}"
export TEST_IMAGE="${TEST_IMAGE:-localhost:5000/mica-uniproton-app:xen-0.1}"

echo "MicRun IO Test Environment:"
echo "  REMOTE_HOST=$REMOTE_HOST"
echo "  TEST_IMAGE=$TEST_IMAGE"
