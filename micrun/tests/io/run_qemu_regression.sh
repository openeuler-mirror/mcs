#!/bin/bash
# One-click qemu regression:
# 1. build arm64 micrun shim from current workspace
# 2. deploy shim to qemu guest
# 3. import RTOS image tar into guest containerd
# 4. run adaptive IO regression

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MICRUN_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"

# shellcheck source=./test_helpers.sh
source "${SCRIPT_DIR}/test_helpers.sh"

QEMU_SHIM_BINARY="${QEMU_SHIM_BINARY:-${MICRUN_DIR}/builds/containerd-shim-mica-v2-arm64}"
QEMU_REMOTE_TMP_DIR="${QEMU_REMOTE_TMP_DIR:-/tmp}"
QEMU_REMOTE_SHIM_PATH="${QEMU_REMOTE_SHIM_PATH:-/usr/bin/containerd-shim-mica-v2}"
QEMU_SOURCE_IMAGE_REF="${QEMU_SOURCE_IMAGE_REF:-localhost:5000/mica-uniproton-app:xen-0.1}"
QEMU_REMOTE_IMAGE_TAR="${QEMU_REMOTE_IMAGE_TAR:-${QEMU_REMOTE_TMP_DIR}/$(basename "${QEMU_IMAGE_TAR:-localhost_5000_mica-uniproton-app_xen-0.1.tar}")}"

log() {
  printf '[qemu-regression] %s\n' "$1"
}

find_default_image_tar() {
  local candidates=()
  local candidate
  local roots=()
  local root
  local found

  if [ -n "${QEMU_OUTPUT_DIR:-}" ]; then
    candidates+=(
      "${QEMU_OUTPUT_DIR}/exports/local_mica-uniproton-app_xen-arm64-0.1.tar"
      "${QEMU_OUTPUT_DIR}/micrun-files/localhost_5000_mica-uniproton-app_xen-0.1.tar"
      "${QEMU_OUTPUT_DIR}/localhost_5001_mica-uniproton-app_xen-0.1.tar"
    )
    roots+=("$QEMU_OUTPUT_DIR" "$(dirname "$QEMU_OUTPUT_DIR")")
  fi

  candidates+=("${MICRUN_DIR}/tests/io/localhost_5001_mica-uniproton-app_xen-0.1.tar")

  for candidate in "${candidates[@]}"; do
    if [ -f "$candidate" ]; then
      printf '%s\n' "$candidate"
      return 0
    fi
  done

  roots+=("$PWD")
  for root in "${roots[@]}"; do
    [ -d "$root" ] || continue
    found="$(
      find "$root" -maxdepth 5 -type f \
        \( -path '*/exports/*mica-uniproton-app*.tar' \
        -o -path '*/micrun-files/*mica-uniproton-app*.tar' \
        -o -name 'localhost_*mica-uniproton-app*.tar' \
        -o -name 'local_mica-uniproton-app*.tar' \) \
        2>/dev/null | sort | tail -n 1
    )"
    if [ -n "$found" ]; then
      printf '%s\n' "$found"
      return 0
    fi
  done
}

build_shim() {
  log "building arm64 shim from current workspace"
  (
    cd "$MICRUN_DIR"
    make build BUILD_ARCH=arm64
  )
}

deploy_shim() {
  if [ ! -f "$QEMU_SHIM_BINARY" ]; then
    printf 'shim binary not found: %s\n' "$QEMU_SHIM_BINARY" >&2
    exit 1
  fi

  log "copying shim to guest: ${QEMU_REMOTE_TMP_DIR}"
  copy_to_remote "$QEMU_SHIM_BINARY" "$REMOTE" "${QEMU_REMOTE_TMP_DIR}/containerd-shim-mica-v2"

  log "installing shim to ${QEMU_REMOTE_SHIM_PATH}"
  remote "
    backup_path='${QEMU_REMOTE_SHIM_PATH}.bak.\$(date +%Y%m%d%H%M%S)'
    if [ -x '${QEMU_REMOTE_SHIM_PATH}' ]; then
      cp -an '${QEMU_REMOTE_SHIM_PATH}' \"\$backup_path\" || true
    fi
    install -m 755 '${QEMU_REMOTE_TMP_DIR}/containerd-shim-mica-v2' '${QEMU_REMOTE_SHIM_PATH}'
    '${QEMU_REMOTE_SHIM_PATH}' --version
  "
}

import_image_tar() {
  local image_tar="${QEMU_IMAGE_TAR:-}"
  if [ -z "$image_tar" ]; then
    image_tar="$(find_default_image_tar)"
  fi

  if [ -z "$image_tar" ] || [ ! -f "$image_tar" ]; then
    cat >&2 <<EOF
image tar not found.
Set QEMU_IMAGE_TAR explicitly, or generate it first via mica-image-builder.py.
Expected example:
  export QEMU_IMAGE_TAR="<path-to-build-output>/<timestamp>/exports/local_mica-uniproton-app_xen-arm64-0.1.tar"
EOF
    exit 1
  fi

  log "copying image tar to guest: $(basename "$image_tar")"
  copy_to_remote "$image_tar" "$REMOTE" "$QEMU_REMOTE_IMAGE_TAR"

  log "importing image tar into guest containerd"
  remote "
    ctr image import '${QEMU_REMOTE_IMAGE_TAR}'
    if ctr images ls | awk '{print \$1}' | grep -Fxq '${QEMU_SOURCE_IMAGE_REF}'; then
      ctr image tag '${QEMU_SOURCE_IMAGE_REF}' '${TEST_IMAGE}' 2>/dev/null || true
    elif ! ctr images ls | awk '{print \$1}' | grep -Fxq '${TEST_IMAGE}'; then
      imported_ref=\"\$(ctr images ls | awk '{print \$1}' | grep -E '(^|/)(mica-uniproton-app|mica-zephyr-app):' | head -n 1 || true)\"
      if [ -n \"\$imported_ref\" ]; then
        ctr image tag \"\$imported_ref\" '${TEST_IMAGE}' 2>/dev/null || true
      fi
    fi
    ctr images ls | grep -E 'mica-uniproton-app|mica-zephyr-app' || true
  "
}

run_io_regression() {
  log "running adaptive IO regression against ${REMOTE}"
  TEST_REMOTE_HOST="$REMOTE" \
  TEST_REMOTE_PASSWORD="${TEST_REMOTE_PASSWORD:-}" \
  TEST_IMAGE="$TEST_IMAGE" \
  NERDCTL_NETWORK_MODE="$NERDCTL_NETWORK_MODE" \
  IMAGE_PROFILE="$IMAGE_PROFILE" \
  bash "${SCRIPT_DIR}/run_all_io_tests.sh"
}

main() {
  if ! remote "echo connected" >/dev/null 2>&1; then
    printf 'cannot connect to qemu guest via ssh: %s\n' "$REMOTE" >&2
    printf 'this script assumes tap0/qemu networking is already configured.\n' >&2
    exit 1
  fi

  log "connected to guest ${REMOTE}"
  ensure_containerd || true
  cleanup_all

  build_shim
  deploy_shim
  import_image_tar

  cleanup_all
  ensure_containerd || true
  run_io_regression
}

main "$@"
