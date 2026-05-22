#!/bin/bash

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${SCRIPT_DIR}/../common/env.sh"
source "${SCRIPT_DIR}/../common/remote.sh"

TEST_REMOTE_HOST="${TEST_REMOTE_HOST:-${REMOTE_HOST:-root@192.168.7.2}}"
REMOTE="$TEST_REMOTE_HOST"
IMAGE="${TEST_IMAGE:-localhost:5000/mica-uniproton-app:xen-0.1}"
NERDCTL_NETWORK_MODE="${NERDCTL_NETWORK_MODE:-none}"
IMAGE_PROFILE="${IMAGE_PROFILE:-auto}"
UNIPROTON_INTERACTION_WAIT_SECS="${UNIPROTON_INTERACTION_WAIT_SECS:-8}"
NATIVE_MICA_TARGET="${NATIVE_MICA_TARGET:-qemu-uniproton-xen}"
NATIVE_FIRMWARE_PATH="${NATIVE_FIRMWARE_PATH:-/lib/firmware/${NATIVE_MICA_TARGET}.elf}"
NATIVE_PROMPT_HEX="${NATIVE_PROMPT_HEX:-6f70656e45756c657220556e6950726f746f6e202320}"
NATIVE_HELP_HEX="${NATIVE_HELP_HEX:-737570706f7274207368656c6c20636f6d6d6f6e643a}"

DETECTED_IMAGE_PROFILE=""
PROFILE_PROBE_OUTPUT=""

sanitize_command_output() {
  tr -d '\000' | tr '\r' '\n' | \
    sed -E 's/\x1B\[[0-9;?]*[[:alpha:]]//g' | \
    awk '
      /^spawn (ssh|sshpass) / { next }
      /^Warning: Permanently added / { next }
      /^time="[^"]+" level=warning msg="failed to parse the containerd version / { next }
      /^Last login:/ { next }
      /^exit$/ { next }
      /^logout$/ { next }
      /openEuler Embedded Reference Distro latest %h/ { banner=1; next }
      banner {
        if ($0 ~ /^Authorized uses only\. All activity may be monitored and reported\.$/) {
          banner=0
        }
        next
      }
      { print }
    '
}

count_shell_markers() {
  local count

  count="$(
    sanitize_command_output | awk '
      /support shell commond/ { count++ }
      /openEuler UniProton #/ { count++ }
      /Available commands:/ { count++ }
      END { print count + 0 }
    '
  )"

  printf '%s\n' "${count:-0}"
}

count_hello_markers() {
  sanitize_command_output | awk '
    /Hello, (UniProton|Zephyr)!/ { count++ }
    END { print count + 0 }
  '
}

has_uniproton_response_markers() {
  local cleaned
  local shell_markers
  local hello_markers

  cleaned="$(sanitize_command_output)"
  shell_markers="$(printf '%s\n' "$cleaned" | count_shell_markers)"
  hello_markers="$(printf '%s\n' "$cleaned" | count_hello_markers)"

  if [ "${shell_markers:-0}" -gt 0 ]; then
    return 0
  fi

  if printf '%s\n' "$cleaned" | grep -Eq '(^|[^[:alpha:]])(help|uname)([^[:alpha:]]|$)'; then
    return 0
  fi

  if [ "${hello_markers:-0}" -ge 2 ]; then
    return 0
  fi

  return 1
}

classify_interaction_output() {
  local output
  local shell_markers

  output="$(sanitize_command_output)"
  shell_markers="$(printf '%s\n' "$output" | count_shell_markers)"

  if [ "${shell_markers:-0}" -gt 0 ]; then
    printf '%s\n' "shell"
    return 0
  fi

  if printf '%s\n' "$output" | expect_hello_output; then
    printf '%s\n' "hello"
    return 0
  fi

  printf '%s\n' "unknown"
}

remote_with_timeout() {
  local seconds="$1"
  shift

  if [ -n "${TEST_REMOTE_PASSWORD:-}" ]; then
    timeout "$seconds" sshpass -p "$TEST_REMOTE_PASSWORD" ssh $(remote_ssh_opts) "$REMOTE" "$1"
  else
    timeout "$seconds" ssh $(remote_ssh_opts) "$REMOTE" "$1"
  fi
}

ensure_containerd() {
  remote "
    if ! systemctl is-active containerd >/dev/null 2>&1; then
      systemctl restart containerd >/dev/null 2>&1 || true
    fi
    i=0
    while [ ! -S /run/containerd/containerd.sock ] && [ \$i -lt 10 ]; do
      sleep 1
      i=\$((i + 1))
    done
    test -S /run/containerd/containerd.sock
  " >/dev/null 2>&1
}

cleanup_container_id() {
  local id="$1"
  remote "
    if command -v mica >/dev/null 2>&1; then
      mica stop $id 2>/dev/null || true
      mica rm $id 2>/dev/null || true
    fi
    ctr task kill -s 9 $id 2>/dev/null || true
    ctr task delete $id 2>/dev/null || true
    ctr container delete $id 2>/dev/null || true
    xl destroy $id 2>/dev/null || true
  " >/dev/null 2>&1 || true
}

cleanup_micad_clients() {
  remote "
    command -v mica >/dev/null 2>&1 || exit 0

    status_file=\$(mktemp /tmp/micrun-mica-status.XXXXXX)
    mica status >\"\$status_file\" 2>/dev/null || true

    awk 'NR > 1 && \$1 != \"\" { print \$1 }' \"\$status_file\" | while read -r id; do
      [ -n \"\$id\" ] || continue
      case \"\$id\" in
        qemu-*)
          mica stop \"\$id\" 2>/dev/null || true
          ;;
        *)
          mica stop \"\$id\" 2>/dev/null || true
          mica rm \"\$id\" 2>/dev/null || true
          xl destroy \"\$id\" 2>/dev/null || true
          ;;
      esac
    done

    if awk 'NR > 1 && \$1 !~ /^qemu-/ { found=1 } END { exit found ? 0 : 1 }' \"\$status_file\"; then
      systemctl restart micad >/dev/null 2>&1 || true
    fi

    rm -f \"\$status_file\"
  " >/dev/null 2>&1 || true
}

cleanup_all() {
  remote "
    pkill -9 ctr 2>/dev/null || true
    pkill -9 nerdctl 2>/dev/null || true
    for pid in \$(pgrep -f '[c]ontainerd-shim-mica-v2' 2>/dev/null || true); do
      kill -9 \$pid 2>/dev/null || true
    done
    sleep 1
    for pass in 1 2 3; do
      for d in \$(xl list 2>/dev/null | awk '{print \$1}' | grep -v '^Name$' | grep -v '^Domain-0$'); do
        xl destroy \$d 2>/dev/null || true
      done
      sleep 1
    done
  " >/dev/null 2>&1 || true

  cleanup_micad_clients

  remote "
    rm -rf /run/containerd/io.containerd.runtime.v2.task/default/* 2>/dev/null || true
    rm -rf /run/micrun/containers/* /run/micrun/runtime/container/* /run/micrun/runtime/sandbox/* 2>/dev/null || true
    systemctl restart containerd >/dev/null 2>&1 || true
    i=0
    while [ ! -S /run/containerd/containerd.sock ] && [ \$i -lt 10 ]; do
      sleep 1
      i=\$((i + 1))
    done
    sleep 2
    pass=0
    while [ \$pass -lt 2 ]; do
      for c in \$(ctr container ls -q 2>/dev/null); do
        ctr container delete \$c 2>/dev/null || true
      done
      sleep 1
      pass=\$((pass + 1))
    done
    i=0
    while [ \$i -lt 10 ]; do
      remaining=\$(xl list 2>/dev/null | awk 'NR > 1 && \$1 != \"Domain-0\" { count++ } END { print count + 0 }')
      [ \"\${remaining:-0}\" = \"0\" ] && break
      for d in \$(xl list 2>/dev/null | awk '{print \$1}' | grep -v '^Name$' | grep -v '^Domain-0$'); do
        xl destroy \$d 2>/dev/null || true
      done
      sleep 1
      i=\$((i + 1))
    done
  " >/dev/null 2>&1 || true
}

expect_shell_output() {
  sanitize_command_output | grep -q "support shell commond"
}

expect_shell_prompt_output() {
  sanitize_command_output | grep -q "openEuler UniProton #"
}

expect_hello_output() {
  sanitize_command_output | grep -Eq "Hello, (UniProton|Zephyr)!"
}

xen_domain_list() {
  remote "
    xl list 2>/dev/null | awk 'NR > 1 && \$1 != \"Domain-0\" { print \$1 }'
  " 2>/dev/null || true
}

xen_domain_count() {
  local count

  count="$(xen_domain_list | sed '/^$/d' | wc -l | tr -d ' ')"
  printf '%s\n' "${count:-0}"
}

native_shell_strings_present() {
  remote "
    test -f '${NATIVE_FIRMWARE_PATH}' &&
    command -v strings >/dev/null 2>&1 &&
    strings '${NATIVE_FIRMWARE_PATH}' | grep -Eq 'openEuler UniProton #|support shell commond|shell init fail|shell is not yet initialized'
  " >/dev/null 2>&1
}

probe_native_mica_shell_hex() {
  remote_with_timeout 60 "
    set -e
    target='${NATIVE_MICA_TARGET}'
    cleanup() {
      mica stop \"\$target\" >/dev/null 2>&1 || true
    }
    trap cleanup EXIT

    capture_hex() {
      real_tty=\"\$1\"
      payload=\"\$2\"
      count=\"\$3\"

      (timeout 3 dd if=\"\$real_tty\" bs=1 count=\"\$count\" status=none | od -An -tx1 -v | tr -d ' \n') & pid=\$!
      sleep 0.2
      printf \"%b\" \"\$payload\" > \"\$real_tty\"
      wait \$pid || true
    }

    mica stop \"\$target\" >/dev/null 2>&1 || true
    mica start \"\$target\" >/dev/null

    ready=0
    for i in \$(seq 1 20); do
      if mica status | grep -q \"\$target.*rpmsg-tty\"; then
        ready=1
        break
      fi
      sleep 1
    done

    [ \"\$ready\" -eq 1 ]

    tty=\"/dev/ttyRPMSG_\${target}_0\"
    [ -L \"\$tty\" ]
    real_tty=\$(readlink -f \"\$tty\")
    stty -F \"\$real_tty\" raw -echo -onlcr -ocrnl -icrnl -inlcr >/dev/null 2>&1 || true

    printf 'ENTER_HEX=%s\n' \"\$(capture_hex \"\$real_tty\" '\\r' 512)\"
    printf 'HELP_HEX=%s\n' \"\$(capture_hex \"\$real_tty\" 'help\\r' 2048)\"
  " 2>/dev/null || true
}

detect_image_profile() {
  local probe_id="micrun-profile-probe"
  local probe_file
  local fallback_file
  local probe_classification

  if [ -n "$DETECTED_IMAGE_PROFILE" ]; then
    echo "$DETECTED_IMAGE_PROFILE"
    return 0
  fi

  if [ "$IMAGE_PROFILE" != "auto" ]; then
    DETECTED_IMAGE_PROFILE="$IMAGE_PROFILE"
    echo "$DETECTED_IMAGE_PROFILE"
    return 0
  fi

  ensure_containerd || true
  cleanup_all
  ensure_containerd || true

  probe_file="$(mktemp /tmp/micrun-io-probe.XXXXXX)"
  remote "
    ctr task kill -s 9 ${probe_id} 2>/dev/null || true
    ctr task delete ${probe_id} 2>/dev/null || true
    ctr container delete ${probe_id} 2>/dev/null || true
    ctr container create --runtime io.containerd.mica.v2 ${IMAGE} ${probe_id} >/dev/null 2>&1
    ctr task start -d ${probe_id} >/dev/null 2>&1
    sleep 3
    (sleep 2; printf 'help\n'; sleep 2) | timeout 12 ctr task attach ${probe_id} 2>&1 || true
    ctr task kill -s 9 ${probe_id} 2>/dev/null || true
    ctr task delete ${probe_id} 2>/dev/null || true
    ctr container delete ${probe_id} 2>/dev/null || true
  " >"$probe_file" 2>&1 || true
  PROFILE_PROBE_OUTPUT="$(sanitize_command_output <"$probe_file")"
  rm -f "$probe_file"
  probe_classification="$(printf '%s\n' "$PROFILE_PROBE_OUTPUT" | classify_interaction_output)"

  if [ "$probe_classification" = "shell" ] || [ "$probe_classification" = "hello" ]; then
    DETECTED_IMAGE_PROFILE="$probe_classification"
  else
    fallback_file="$(mktemp /tmp/micrun-io-fallback.XXXXXX)"
    remote_with_timeout 25 "
      timeout 20 sh -c '(sleep 2; echo help; sleep 2) | nerdctl run -i --rm --network=${NERDCTL_NETWORK_MODE} --runtime io.containerd.mica.v2 ${IMAGE}' 2>&1 || true
    " >"$fallback_file" 2>&1 || true
    local ctr_fallback
    ctr_fallback="$(sanitize_command_output <"$fallback_file")"
    rm -f "$fallback_file"
    PROFILE_PROBE_OUTPUT="${PROFILE_PROBE_OUTPUT}
--- ctr fallback ---
${ctr_fallback}"
    PROFILE_PROBE_OUTPUT="$(printf '%s' "$PROFILE_PROBE_OUTPUT" | sanitize_command_output)"
    DETECTED_IMAGE_PROFILE="$(printf '%s\n' "$PROFILE_PROBE_OUTPUT" | classify_interaction_output)"
  fi

  if native_shell_strings_present &&
     { [ "$DETECTED_IMAGE_PROFILE" = "hello" ] || [ "$DETECTED_IMAGE_PROFILE" = "unknown" ]; }; then
    DETECTED_IMAGE_PROFILE="shell-regressed"
  fi

  cleanup_all

  echo "$DETECTED_IMAGE_PROFILE"
}

image_profile_is() {
  local expected="$1"
  [ "$(detect_image_profile)" = "$expected" ]
}

image_profile_is_shell_family() {
  local actual

  actual="$(detect_image_profile)"
  [ "$actual" = "shell" ] || [ "$actual" = "shell-regressed" ]
}
