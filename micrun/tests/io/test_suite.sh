#!/bin/bash
# MicRun IO Test Suite - adaptive smoke tests for shell and hello-style images.

set -u

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=./test_helpers.sh
source "$SCRIPT_DIR/test_helpers.sh"

PASS='\033[0;32m✓ PASS\033[0m'
FAIL='\033[0;31m✗ FAIL\033[0m'
SKIP='\033[1;33m- SKIP\033[0m'

expect_any_output() {
  expect_shell_output || expect_hello_output
}

failure_summary_line() {
  local output="$1"
  local line

  # Prefer the expect script's own marker over later nerdctl cleanup noise
  # such as "failed to remove container ... created".
  line="$(printf '%s\n' "$output" | grep -E \
    'TTY_FAIL|ATTACH_DETACH_FAIL|TTY_UX_MATRIX_FAIL|CTR_AUTO_CLOSE_FAIL|CTR_MANUAL_EXIT_FAIL' | head -1)"
  if [ -n "$line" ]; then
    printf '%s\n' "$line"
    return 0
  fi

  printf '%s\n' "$output" | grep -E \
    'FATA|ERRO|failed to start IO|failed to start shim|connection refused|CREATE_EC=[1-9]|START_EC=[1-9]' | head -1
}

section_count() {
  local section="$1"

  awk -v section="$section" '
    $0 == section {
      found=1
      in_section=1
      next
    }
    in_section {
      if ($0 ~ /^[[:space:]]*[0-9]+[[:space:]]*$/) {
        gsub(/[[:space:]]/, "", $0)
        print $0
        exit
      }
      if ($0 ~ /^--- /) {
        print 0
        exit
      }
    }
    END {
      if (!found) {
        print 0
      }
    }
  '
}

cleanup_sections_are_zero() {
  local output="$1"
  local tasks
  local containers

  tasks="$(printf '%s\n' "$output" | section_count '--- tasks ---')"
  containers="$(printf '%s\n' "$output" | section_count '--- containers ---')"

  [ "${tasks:-0}" = "0" ] && [ "${containers:-0}" = "0" ]
}

is_transient_shim_bootstrap_failure() {
  local output="$1"

  printf '%s\n' "$output" | grep -Eq \
    'failed to start shim: start failed: failed to create TTRPC connection|failed to create TTRPC connection|connection refused: unknown'
}

run_tty_expect() {
  local seconds="$1"
  local script="$2"
  local cid="$3"

  env \
    TEST_REMOTE_HOST="$TEST_REMOTE_HOST" \
    TEST_REMOTE_PORT="${TEST_REMOTE_PORT:-}" \
    TEST_REMOTE_PASSWORD="${TEST_REMOTE_PASSWORD:-}" \
    TTY_TEST_IMAGE="$IMAGE" \
    TTY_TEST_CONTAINER_NAME="$cid" \
    NERDCTL_NETWORK_MODE="$NERDCTL_NETWORK_MODE" \
    timeout "$seconds" expect "$script"
}

run_tty_expect_with_transient_retry() {
  local seconds="$1"
  local script="$2"
  local cid_prefix="$3"
  local cid
  local out
  local clean

  cid="${cid_prefix}-$RANDOM"
  cleanup_container_id "$cid"
  out="$(run_tty_expect "$seconds" "$script" "$cid" 2>&1 || true)"
  cleanup_between_tests
  clean="$(printf '%s\n' "$out" | sanitize_command_output)"

  if is_transient_shim_bootstrap_failure "$clean"; then
    ensure_containerd || true
    sleep 1
    cid="${cid_prefix}-$RANDOM"
    cleanup_container_id "$cid"
    out="$(run_tty_expect "$seconds" "$script" "$cid" 2>&1 || true)"
    cleanup_between_tests
    clean="$(printf '%s\n' "$out" | sanitize_command_output)"
  fi

  printf '%s\n' "$clean"
}

run_hello_notty_probe() {
  local cid="$1"

  remote_with_timeout 25 "
    : > /var/log/mica/mica-runtime.log
    nerdctl rm -f $cid 2>/dev/null || true
    timeout 30 sh -c '(sleep ${UNIPROTON_INTERACTION_WAIT_SECS}; printf '\''help
uname
'\''; sleep 6) | nerdctl run -i --rm --name $cid --network=${NERDCTL_NETWORK_MODE} --runtime io.containerd.mica.v2 $IMAGE' 2>&1 || true
    echo '--- post-input ---'
    grep -c 'Observed output after stdin for ' /var/log/mica/mica-runtime.log || true
  " 2>&1 || true
}

run_shell_notty_probe() {
  local cid="$1"

  remote_with_timeout 50 "
    nerdctl rm -f $cid 2>/dev/null || true
    timeout 40 sh -c '(sleep 8; echo help; echo uname; sleep 5) | nerdctl run -i --rm --name $cid --network=${NERDCTL_NETWORK_MODE} --runtime io.containerd.mica.v2 $IMAGE' 2>&1 || true
  " 2>&1 || true
}

run_probe_with_transient_retry() {
  local cid_prefix="$1"
  local probe_func="$2"
  local cid
  local out
  local clean

  cid="${cid_prefix}-$RANDOM"
  cleanup_container_id "$cid"
  out="$("$probe_func" "$cid")"
  cleanup_between_tests
  clean="$(printf '%s\n' "$out" | sanitize_command_output)"

  if is_transient_shim_bootstrap_failure "$clean"; then
    ensure_containerd || true
    sleep 1
    cid="${cid_prefix}-$RANDOM"
    cleanup_container_id "$cid"
    out="$("$probe_func" "$cid")"
    cleanup_between_tests
    clean="$(printf '%s\n' "$out" | sanitize_command_output)"
  fi

  printf '%s\n' "$clean"
}

test_native_mica_shell_preflight() {
  echo -n "Test 0: native mica shell prompt... "
  local out
  local enter_hex
  local help_hex

  if ! native_shell_strings_present; then
    echo -e "$SKIP (no shell markers in ${NATIVE_FIRMWARE_PATH})"
    return 2
  fi

  out="$(probe_native_mica_shell_hex)"
  enter_hex="$(printf '%s\n' "$out" | sed -n 's/^ENTER_HEX=//p' | tail -n 1)"
  help_hex="$(printf '%s\n' "$out" | sed -n 's/^HELP_HEX=//p' | tail -n 1)"

  if printf '%s' "$enter_hex" | grep -qi "$NATIVE_PROMPT_HEX" &&
     printf '%s' "$help_hex" | grep -qi "$NATIVE_HELP_HEX" &&
     printf '%s' "$help_hex" | grep -qi "$NATIVE_PROMPT_HEX"; then
    echo -e "$PASS"
    return 0
  fi

  echo -e "$FAIL"
  echo "  ENTER_HEX=${enter_hex:-<empty>}"
  echo "  HELP_HEX=${help_hex:-<empty>}"
  return 1
}

test_shell_ctr_background() {
  echo -n "Test 1: ctr background mode attach... "
  local out
  local clean
  local cid="shell-bg-$RANDOM"
  local create_ec
  local start_ec
  ensure_containerd || true
  prepare_io_case
  cleanup_container_id "$cid"
  out="$(run_ctr_bg_attach "$cid" help)"
  clean="$(printf '%s\n' "$out" | sanitize_command_output)"
  create_ec="$(printf '%s\n' "$clean" | sed -n 's/^CREATE_EC=//p' | tail -1)"
  start_ec="$(printf '%s\n' "$clean" | sed -n 's/^START_EC=//p' | tail -1)"

  if [ "${create_ec:-1}" = "0" ] && [ "${start_ec:-1}" = "0" ] &&
     printf '%s\n' "$clean" | expect_shell_output &&
     printf '%s\n' "$clean" | expect_shell_prompt_output; then
    echo -e "$PASS"
    return 0
  fi

  echo -e "$FAIL"
  local failure_line
  failure_line="$(failure_summary_line "$clean")"
  [ -n "$failure_line" ] && echo "  Failure: $failure_line"
  echo "  create=${create_ec:-?} start=${start_ec:-?}"
  echo "  Output: $(printf '%s\n' "$clean" | awk '/^--- attach ---/{p=1;next} p' | tr '\n' ' ' | tail -c 240)"
  return 1
}

test_shell_nerdctl_tty() {
  echo -n "Test 2: nerdctl TTY mode (-it)... "
  local out
  local clean
  local failure_line
  ensure_containerd || true
  prepare_io_case
  clean="$(run_tty_expect_with_transient_retry 75 "$SCRIPT_DIR/test_nerdctl_tty_run.exp" "micrun-tty")"

  if printf '%s\n' "$clean" | grep -q '^TTY_PASS$'; then
    echo -e "$PASS"
    return 0
  fi

  echo -e "$FAIL"
  failure_line="$(failure_summary_line "$clean")"
  [ -n "$failure_line" ] && echo "  Failure: $failure_line"
  echo "  Output: $(echo "$clean" | head -6)"
  return 1
}

test_shell_nerdctl_attach_detach() {
  echo -n "Test 3: nerdctl attach/detach lifecycle... "
  local out
  local clean
  local failure_line
  ensure_containerd || true
  prepare_io_case
  clean="$(run_tty_expect_with_transient_retry 75 "$SCRIPT_DIR/test_nerdctl_attach_detach.exp" "micrun-attach")"

  if printf '%s\n' "$clean" | grep -q '^ATTACH_DETACH_PASS$'; then
    echo -e "$PASS"
    return 0
  fi

  echo -e "$FAIL"
  failure_line="$(failure_summary_line "$clean")"
  [ -n "$failure_line" ] && echo "  Failure: $failure_line"
  echo "  Output: $(echo "$clean" | head -8)"
  return 1
}

test_shell_nerdctl_tty_ux_matrix() {
  echo -n "Test 4: nerdctl run -it UX matrix... "
  local out
  local clean
  local failure_line
  ensure_containerd || true
  prepare_io_case
  clean="$(run_tty_expect_with_transient_retry 180 "$SCRIPT_DIR/test_nerdctl_tty_ux_matrix.exp" "micrun-tty-ux")"

  if printf '%s\n' "$clean" | grep -q '^TTY_UX_MATRIX_PASS$'; then
    echo -e "$PASS"
    return 0
  fi

  echo -e "$FAIL"
  failure_line="$(failure_summary_line "$clean")"
  [ -n "$failure_line" ] && echo "  Failure: $failure_line"
  echo "  Output: $(echo "$clean" | head -10)"
  return 1
}

test_shell_nerdctl_notty() {
  echo -n "Test 5: nerdctl non-TTY mode (-i)... "
  local clean
  ensure_containerd || true
  prepare_io_case
  clean="$(run_probe_with_transient_retry "shell-nerd-notty" run_shell_notty_probe)"

  if printf '%s\n' "$clean" | expect_shell_output &&
     printf '%s\n' "$clean" | expect_shell_prompt_output; then
    echo -e "$PASS"
    return 0
  fi

  echo -e "$FAIL"
  echo "  Output: $(echo "$clean" | head -5)"
  return 1
}

test_shell_notty_xen_cleanup() {
  echo -n "Test 6: non-TTY path leaves no stale Xen domain... "
  local before_count
  local after_count
  local after_list
  local after_norm
  local out
  local clean

  ensure_containerd || true
  prepare_io_case

  before_count="$(xen_domain_count)"
  if [ "${before_count:-0}" != "0" ]; then
    echo -e "$FAIL"
    echo "  Domains before test after cleanup: ${before_count:-0}"
    return 1
  fi

  out="$(remote_with_timeout 50 "
    nerdctl rm -f shell-notty-leak 2>/dev/null || true
    timeout 20 sh -c '(echo help; sleep 2) | nerdctl run -i --rm --name shell-notty-leak --network=${NERDCTL_NETWORK_MODE} --runtime io.containerd.mica.v2 $IMAGE' 2>&1 || true
  " 2>&1 || true)"
  clean="$(printf '%s\n' "$out" | sanitize_command_output)"

  sleep 2
  after_list="$(xen_domain_list)"
  after_count="$(printf '%s\n' "$after_list" | sed '/^$/d' | wc -l | tr -d ' ')"
  after_norm="$(printf '%s\n' "$after_list" | sed '/^$/d' | sort | tr '\n' ' ')"
  cleanup_between_tests

  # Require proof the container actually ran (shell response or banner):
  # a failed run leaves no domain either, which used to pass vacuously.
  if [ "${after_count:-0}" = "0" ] &&
     printf '%s\n' "$clean" | grep -qiE 'Available commands|command not found|openEuler|UniProton'; then
    echo -e "$PASS"
    return 0
  fi

  echo -e "$FAIL"
  echo "  Domains after non-TTY run: ${after_count:-0}"
  [ -n "$after_list" ] && echo "  After: $(echo "$after_list" | tr '\n' ' ' | sed 's/[[:space:]]\+$//')"
  [ -n "$after_norm" ] && echo "  Normalized: $after_norm"
  echo "  Output: $(echo "$clean" | head -5)"
  return 1
}

run_shell_nerdctl_create_start_stop_probe() {
  local cid="$1"

  remote_with_timeout 90 "
    nerdctl rm -f $cid 2>/dev/null || true
    nerdctl create -i -t --name $cid --runtime io.containerd.mica.v2 --network=${NERDCTL_NETWORK_MODE} -l org.openeuler.micrun.container.auto_close=false $IMAGE
    nerdctl start $cid
    sleep 3
    nid=\$(nerdctl inspect $cid | sed -n 's/.*\"Id\": \"\\([^\"]*\\)\".*/\\1/p' | head -1)
    echo '--- ps ---'
    nerdctl ps | grep -c $cid || true
    echo '--- id ---'
    [ -n \"\$nid\" ] && echo 1 || echo 0
    echo '--- task ---'
    ctr task ls | awk -v id=\"\$nid\" '\$1 == id && \$3 == \"RUNNING\" { c++ } END { print c + 0 }'
    echo '--- xen ---'
    xl list | awk -v id=\"\$nid\" '\$1 == id { c++ } END { print c + 0 }'
    echo '--- stop ---'
    nerdctl stop -t 5 $cid 2>&1 || true
    sleep 2
    echo '--- post-running ---'
    ctr task ls | awk -v id=\"\$nid\" '\$1 == id && \$3 == \"RUNNING\" { c++ } END { print c + 0 }'
    echo '--- post-xen ---'
    xl list | awk -v id=\"\$nid\" '\$1 == id { c++ } END { print c + 0 }'
    echo '--- rm ---'
    nerdctl rm $cid 2>&1 || true
    sleep 1
    echo '--- post-tasks ---'
    ctr task ls | awk -v id=\"\$nid\" '\$1 == id { c++ } END { print c + 0 }'
    echo '--- post-containers ---'
    ctr container ls | awk -v id=\"\$nid\" '\$1 == id { c++ } END { print c + 0 }'
  " 2>&1 || true
}

test_shell_nerdctl_create_start_stop() {
  echo -n "Test 7: nerdctl create/start/stop/rm lifecycle... "
  local clean
  local ps_count
  local id_count
  local task_count
  local xen_count
  local post_running
  local post_tasks
  local post_xen
  local post_containers

  ensure_containerd || true
  prepare_io_case
  clean="$(run_probe_with_transient_retry "shell-create" run_shell_nerdctl_create_start_stop_probe)"

  ps_count="$(printf '%s\n' "$clean" | section_count '--- ps ---')"
  id_count="$(printf '%s\n' "$clean" | section_count '--- id ---')"
  task_count="$(printf '%s\n' "$clean" | section_count '--- task ---')"
  xen_count="$(printf '%s\n' "$clean" | section_count '--- xen ---')"
  post_running="$(printf '%s\n' "$clean" | section_count '--- post-running ---')"
  post_tasks="$(printf '%s\n' "$clean" | section_count '--- post-tasks ---')"
  post_xen="$(printf '%s\n' "$clean" | section_count '--- post-xen ---')"
  post_containers="$(printf '%s\n' "$clean" | section_count '--- post-containers ---')"

  if [ "${ps_count:-0}" = "1" ] &&
     [ "${id_count:-0}" = "1" ] &&
     [ "${task_count:-0}" = "1" ] &&
     [ "${xen_count:-0}" = "1" ] &&
     [ "${post_running:-1}" = "0" ] &&
     [ "${post_tasks:-1}" = "0" ] &&
     [ "${post_xen:-1}" = "0" ] &&
     [ "${post_containers:-1}" = "0" ]; then
    echo -e "$PASS"
    return 0
  fi

  echo -e "$FAIL"
  echo "  Counts: ps=${ps_count:-?} id=${id_count:-?} task=${task_count:-?} xen=${xen_count:-?} post_running=${post_running:-?} post_tasks=${post_tasks:-?} post_xen=${post_xen:-?} post_containers=${post_containers:-?}"
  echo "  Output: $(echo "$clean" | head -10)"
  return 1
}

run_shell_ctr_lifecycle_status_probe() {
  local cid="$1"

  remote_with_timeout 70 "
    ctr container create --runtime io.containerd.mica.v2 $IMAGE $cid >/dev/null
    ctr task start -d $cid >/dev/null
    sleep 3
    echo '--- task ---'
    ctr task ls | awk '\$1 == \"$cid\" && \$3 == \"RUNNING\" { c++ } END { print c + 0 }'
    echo '--- container ---'
    ctr container ls | awk '\$1 == \"$cid\" { c++ } END { print c + 0 }'
    echo '--- xen ---'
    xl list | awk '\$1 == \"$cid\" { c++ } END { print c + 0 }'
    ctr task kill -s 9 $cid 2>/dev/null || true
    sleep 2
    ctr task delete $cid 2>/dev/null || true
    ctr container delete $cid 2>/dev/null || true
    echo '--- post-tasks ---'
    ctr task ls | awk '\$1 == \"$cid\" { c++ } END { print c + 0 }'
    echo '--- post-containers ---'
    ctr container ls | awk '\$1 == \"$cid\" { c++ } END { print c + 0 }'
    echo '--- post-xen ---'
    xl list | awk '\$1 == \"$cid\" { c++ } END { print c + 0 }'
  " 2>&1 || true
}

test_shell_ctr_lifecycle_status() {
  echo -n "Test 8: ctr lifecycle status and cleanup... "
  local clean
  local task_count
  local container_count
  local xen_count
  local post_tasks
  local post_containers
  local post_xen

  ensure_containerd || true
  prepare_io_case
  clean="$(run_probe_with_transient_retry "shell-ctr-life" run_shell_ctr_lifecycle_status_probe)"

  task_count="$(printf '%s\n' "$clean" | section_count '--- task ---')"
  container_count="$(printf '%s\n' "$clean" | section_count '--- container ---')"
  xen_count="$(printf '%s\n' "$clean" | section_count '--- xen ---')"
  post_tasks="$(printf '%s\n' "$clean" | section_count '--- post-tasks ---')"
  post_containers="$(printf '%s\n' "$clean" | section_count '--- post-containers ---')"
  post_xen="$(printf '%s\n' "$clean" | section_count '--- post-xen ---')"

  if [ "${task_count:-0}" = "1" ] &&
     [ "${container_count:-0}" = "1" ] &&
     [ "${xen_count:-0}" = "1" ] &&
     [ "${post_tasks:-1}" = "0" ] &&
     [ "${post_containers:-1}" = "0" ] &&
     [ "${post_xen:-1}" = "0" ]; then
    echo -e "$PASS"
    return 0
  fi

  echo -e "$FAIL"
  echo "  Counts: task=${task_count:-?} container=${container_count:-?} xen=${xen_count:-?} post_tasks=${post_tasks:-?} post_containers=${post_containers:-?} post_xen=${post_xen:-?}"
  echo "  Output: $(echo "$clean" | head -10)"
  return 1
}

run_shell_user_diagnostics_probe() {
  local cid="$1"

  remote_with_timeout 80 "
    : > /var/log/mica/mica-runtime.log
    nerdctl rm -f $cid 2>/dev/null || true
    nerdctl run -dt --name $cid --runtime io.containerd.mica.v2 --network=${NERDCTL_NETWORK_MODE} -l org.openeuler.micrun.container.auto_close=false $IMAGE >/dev/null
    sleep 3
    nid=\$(nerdctl inspect $cid | sed -n 's/.*\"Id\": \"\\([^\"]*\\)\".*/\\1/p' | head -1)
    echo '--- inspect ---'
    nerdctl inspect $cid | grep -c '\"Name\".*$cid\\|\"ID\"\\|\"Id\"' || true
    echo '--- ctr-info ---'
    ctr containers info \"\$nid\" | grep -c 'io.containerd.mica.v2' || true
    echo '--- log ---'
    tail -300 /var/log/mica/mica-runtime.log | grep -c \"\$nid\\|$cid\" || true
    echo '--- xen ---'
    xl list | awk -v id=\"\$nid\" '\$1 == id { c++ } END { print c + 0 }'
    nerdctl rm -f $cid >/dev/null 2>&1 || true
    sleep 2
    echo '--- post-tasks ---'
    ctr task ls | awk -v id=\"\$nid\" '\$1 == id { c++ } END { print c + 0 }'
    echo '--- post-containers ---'
    ctr container ls | awk -v id=\"\$nid\" '\$1 == id { c++ } END { print c + 0 }'
    echo '--- post-xen ---'
    xl list | awk -v id=\"\$nid\" '\$1 == id { c++ } END { print c + 0 }'
  " 2>&1 || true
}

test_shell_user_diagnostics() {
  echo -n "Test 9: user diagnostics via inspect/info/log/xl... "
  local clean
  local inspect_count
  local ctr_info_count
  local log_count
  local xen_count
  local post_tasks
  local post_containers
  local post_xen

  ensure_containerd || true
  prepare_io_case
  clean="$(run_probe_with_transient_retry "shell-diag" run_shell_user_diagnostics_probe)"

  inspect_count="$(printf '%s\n' "$clean" | section_count '--- inspect ---')"
  ctr_info_count="$(printf '%s\n' "$clean" | section_count '--- ctr-info ---')"
  log_count="$(printf '%s\n' "$clean" | section_count '--- log ---')"
  xen_count="$(printf '%s\n' "$clean" | section_count '--- xen ---')"
  post_tasks="$(printf '%s\n' "$clean" | section_count '--- post-tasks ---')"
  post_containers="$(printf '%s\n' "$clean" | section_count '--- post-containers ---')"
  post_xen="$(printf '%s\n' "$clean" | section_count '--- post-xen ---')"

  if [ "${inspect_count:-0}" -gt 0 ] &&
     [ "${ctr_info_count:-0}" -gt 0 ] &&
     [ "${log_count:-0}" -gt 0 ] &&
     [ "${xen_count:-0}" = "1" ] &&
     [ "${post_tasks:-1}" = "0" ] &&
     [ "${post_containers:-1}" = "0" ] &&
     [ "${post_xen:-1}" = "0" ]; then
    echo -e "$PASS"
    return 0
  fi

  echo -e "$FAIL"
  echo "  Counts: inspect=${inspect_count:-?} ctr_info=${ctr_info_count:-?} log=${log_count:-?} xen=${xen_count:-?} post_tasks=${post_tasks:-?} post_containers=${post_containers:-?} post_xen=${post_xen:-?}"
  echo "  Output: $(echo "$clean" | head -10)"
  return 1
}

test_shell_multiple_commands() {
  echo -n "Test 10: Multiple command execution... "
  local out
  local clean
  local matches
  local cid="shell-multi-$RANDOM"
  ensure_containerd || true
  prepare_io_case
  cleanup_container_id "$cid"
  out="$(run_ctr_bg_attach "$cid" help uname)"
  clean="$(printf '%s\n' "$out" | sanitize_command_output)"

  matches="$(printf '%s\n' "$clean" | count_shell_markers)"
  if [ "$matches" -ge 2 ] && printf '%s\n' "$clean" | expect_shell_output; then
    echo -e "$PASS"
    return 0
  fi

  echo -e "$FAIL"
  local failure_line
  failure_line="$(failure_summary_line "$clean")"
  [ -n "$failure_line" ] && echo "  Failure: $failure_line"
  echo "  Matches: $matches, Output: $(echo "$clean" | head -5)"
  return 1
}

test_shell_log_cleanliness() {
  echo -n "Test 11: Log cleanliness... "
  local count
  local cid="shell-log-$RANDOM"
  ensure_containerd || true
  prepare_io_case
  cleanup_container_id "$cid"
  count="$(remote "
    ctr container create --runtime io.containerd.mica.v2 $IMAGE $cid >/dev/null 2>&1
    ctr task start -d $cid >/dev/null 2>&1
    sleep 3
    ctr task ls | awk -v c="$cid" '$1 == c {print $3; found=1} END {if (!found) print "GONE"}'
    echo help | timeout 8 ctr task attach $cid >/dev/null 2>&1 || true
    sleep 1
    tail -100 /var/log/mica/mica-runtime.log | grep -c 'stdin FIFO read' || echo 0
    ctr task kill -s 9 $cid 2>/dev/null || true
    ctr task delete $cid 2>/dev/null || true
    ctr container delete $cid 2>/dev/null || true
  " 2>/dev/null)"

  run_state="$(echo "$count" | head -1 | tr -d '[:space:]')"
  count="$(echo "$count" | tr -d '\n ' | grep -oE '[0-9]+' | head -1 || echo 0)"

  # GONE/STOPPED means the container never ran: vacuous pass guard.
  if [ "$run_state" = "RUNNING" ] && [ "${count:-0}" -lt 5 ]; then
    echo -e "$PASS (${count} logs)"
    return 0
  fi

  echo -e "$FAIL (state=${run_state:-<none>}, ${count} logs)"
  return 1
}

test_shell_exit_command() {
  echo -n "Test 12: Exit command detection... "
  local out
  local create_ec
  local start_ec
  local left
  local cid="shell-exit-$RANDOM"
  ensure_containerd || true
  prepare_io_case
  cleanup_container_id "$cid"
  out="$(remote_output "
    set +e
    ctr container create --runtime io.containerd.mica.v2 --annotation org.openeuler.micrun.container.auto_close=false $IMAGE $cid
    echo CREATE_EC=\$?
    ctr task start -d $cid
    echo START_EC=\$?
    i=0
    while [ \$i -lt 12 ]; do
      mica status 2>/dev/null | grep -q \"$cid.*rpmsg-tty\" && break
      sleep 1
      i=\$((i + 1))
    done
    (sleep ${UNIPROTON_INTERACTION_WAIT_SECS}; echo help; echo exit; sleep 3) | timeout 15 ctr task attach $cid >/dev/null 2>&1 || true
    sleep 1
    echo LEFT=\$(ctr task ls | awk '\$1 == \"$cid\" { c++ } END { print c + 0 }')
    ctr task kill -s 9 $cid 2>/dev/null || true
    ctr task delete $cid 2>/dev/null || true
    ctr container delete $cid 2>/dev/null || true
  ")"
  create_ec="$(printf '%s\n' "$out" | sed -n 's/^CREATE_EC=//p' | tail -1)"
  start_ec="$(printf '%s\n' "$out" | sed -n 's/^START_EC=//p' | tail -1)"
  left="$(printf '%s\n' "$out" | sed -n 's/^LEFT=//p' | tail -1)"

  if [ "${create_ec:-1}" = "0" ] && [ "${start_ec:-1}" = "0" ] && [ "${left:-1}" = "0" ]; then
    echo -e "$PASS (container stopped)"
    return 0
  fi

  echo -e "$FAIL (create=${create_ec:-?} start=${start_ec:-?} left=${left:-?})"
  return 1
}

test_shell_ctr_foreground_auto_close() {
  echo -n "Test 13: ctr foreground auto_close stops cleanly... "
  local clean
  local failure_line

  ensure_containerd || true
  prepare_io_case
  clean="$(run_tty_expect_with_transient_retry 75 "$SCRIPT_DIR/test_ctr_foreground_auto_close.exp" "micrun-ctr-auto")"

  if printf '%s\n' "$clean" | grep -q '^CTR_AUTO_CLOSE_PASS$'; then
    echo -e "$PASS"
    return 0
  fi

  echo -e "$FAIL"
  failure_line="$(failure_summary_line "$clean")"
  [ -n "$failure_line" ] && echo "  Failure: $failure_line"
  echo "  Output: $(echo "$clean" | head -10)"
  return 1
}

test_shell_ctr_foreground_manual_exit() {
  echo -n "Test 14: ctr foreground manual exit stays clean and restartable... "
  local clean
  local failure_line

  ensure_containerd || true
  prepare_io_case
  clean="$(run_tty_expect_with_transient_retry 95 "$SCRIPT_DIR/test_ctr_foreground_manual_exit.exp" "micrun-ctr-exit")"

  if printf '%s\n' "$clean" | grep -q '^CTR_MANUAL_EXIT_PASS$'; then
    echo -e "$PASS"
    return 0
  fi

  echo -e "$FAIL"
  failure_line="$(failure_summary_line "$clean")"
  [ -n "$failure_line" ] && echo "  Failure: $failure_line"
  echo "  Output: $(echo "$clean" | head -10)"
  return 1
}

test_hello_ctr_background_running() {
  echo -n "Test 1: ctr background start reaches RUNNING... "
  local out
  cleanup_container_id "t1"
  out="$(remote "
    ctr container create --runtime io.containerd.mica.v2 $IMAGE t1 >/dev/null 2>&1
    ctr task start -d t1 >/dev/null 2>&1
    sleep 2
    ctr task ls | grep t1 || true
    ctr task kill -s 9 t1 2>/dev/null || true
    ctr task delete t1 2>/dev/null || true
    ctr container delete t1 2>/dev/null || true
  " 2>&1)"

  if echo "$out" | grep -Eq 't1[[:space:]]+[0-9]+[[:space:]]+RUNNING'; then
    echo -e "$PASS"
    return 0
  fi

  echo -e "$FAIL"
  echo "  Output: $(echo "$out" | tail -3)"
  return 1
}

test_hello_ctr_foreground_output() {
  echo -n "Test 2: nerdctl --rm emits startup output and cleans up... "
  local out
  local clean
  cleanup_container_id "hello-auto"
  out="$(remote "
    nerdctl rm -f hello-auto 2>/dev/null || true
    timeout 20 sh -c '(sleep 4; echo help; sleep 2) | nerdctl run -i --rm --name hello-auto --network=${NERDCTL_NETWORK_MODE} -l org.openeuler.micrun.container.auto_close_timeout=5s --runtime io.containerd.mica.v2 $IMAGE' 2>&1 || true
    echo '--- tasks ---'
    ctr task ls | grep -c hello-auto || true
    echo '--- containers ---'
    ctr container ls | grep -c hello-auto || true
  " 2>&1)"
  clean="$(printf '%s\n' "$out" | sanitize_command_output)"

  if printf '%s\n' "$clean" | expect_hello_output &&
     cleanup_sections_are_zero "$clean"; then
    echo -e "$PASS"
    return 0
  fi

  echo -e "$FAIL"
  echo "  Output: $(echo "$clean" | head -5)"
  return 1
}

test_hello_nerdctl_create_start() {
  echo -n "Test 3: nerdctl create/start lifecycle... "
  local out
  local clean
  cleanup_container_id "hello-create"
  out="$(remote "
    nerdctl rm -f hello-create 2>/dev/null || true
    ctr task kill -s 9 hello-create 2>/dev/null || true
    ctr task delete hello-create 2>/dev/null || true
    ctr container delete hello-create 2>/dev/null || true
    nerdctl create --name hello-create --runtime io.containerd.mica.v2 --network=${NERDCTL_NETWORK_MODE} -l org.openeuler.micrun.container.auto_close_timeout=5s $IMAGE >/tmp/hello-create.out
    cat /tmp/hello-create.out
    nerdctl start hello-create >/tmp/hello-create-start.out 2>&1 || true
    cat /tmp/hello-create-start.out
    sleep 3
    nerdctl ps | grep -q hello-create && echo PS_OK || echo PS_MISS
    nerdctl rm -f hello-create >/tmp/hello-create-rm.out 2>&1 || true
    cat /tmp/hello-create-rm.out
    echo '--- tasks ---'
    ctr task ls | grep -c hello-create || true
    echo '--- containers ---'
    ctr container ls | grep -c hello-create || true
  " 2>&1)"
  clean="$(printf '%s\n' "$out" | sanitize_command_output)"

  # PS_OK proves the container was listed as running; matching the raw
  # name anywhere used to pass on error messages containing the name.
  if printf '%s\n' "$clean" | grep -q 'PS_OK' &&
     ! printf '%s\n' "$clean" | grep -q 'PS_MISS' &&
     cleanup_sections_are_zero "$clean"; then
    echo -e "$PASS"
    return 0
  fi

  echo -e "$FAIL"
  echo "  Output: $(echo "$clean" | head -8)"
  return 1
}

test_hello_nerdctl_detached_lifecycle() {
  echo -n "Test 4: nerdctl run -d/ps/rm lifecycle... "
  local out
  local clean
  cleanup_container_id "hello-det"
  out="$(remote "
    nerdctl rm -f hello-det 2>/dev/null || true
    ctr task kill -s 9 hello-det 2>/dev/null || true
    ctr task delete hello-det 2>/dev/null || true
    ctr container delete hello-det 2>/dev/null || true
    nerdctl run -d --name hello-det --runtime io.containerd.mica.v2 --network=${NERDCTL_NETWORK_MODE} -l org.openeuler.micrun.container.auto_close_timeout=5s $IMAGE
    sleep 3
    nerdctl ps | grep -q hello-det && echo PS_OK || echo PS_MISS
    nerdctl rm -f hello-det >/tmp/hello-det-rm.out 2>&1 || true
    cat /tmp/hello-det-rm.out
    echo '--- tasks ---'
    ctr task ls | grep -c hello-det || true
    echo '--- containers ---'
    ctr container ls | grep -c hello-det || true
  " 2>&1)"
  clean="$(printf '%s\n' "$out" | sanitize_command_output)"

  if printf '%s\n' "$clean" | grep -q 'PS_OK' &&
     ! printf '%s\n' "$clean" | grep -q 'PS_MISS' &&
     cleanup_sections_are_zero "$clean"; then
    echo -e "$PASS"
    return 0
  fi

  echo -e "$FAIL"
  echo "  Output: $(echo "$clean" | head -8)"
  return 1
}

test_hello_nerdctl_notty_output() {
  echo -n "Test 5: nerdctl non-TTY delayed interactive commands... "
  local out
  local clean
  local cid="hello-notty-$RANDOM"
  local observed_after_stdin
  cleanup_container_id "$cid"
  out="$(run_hello_notty_probe "$cid")"
  cleanup_between_tests
  clean="$(printf '%s\n' "$out" | sanitize_command_output)"

  if is_transient_shim_bootstrap_failure "$clean"; then
    ensure_containerd || true
    sleep 1
    cid="hello-notty-$RANDOM"
    cleanup_container_id "$cid"
    out="$(run_hello_notty_probe "$cid")"
    cleanup_between_tests
    clean="$(printf '%s\n' "$out" | sanitize_command_output)"
  fi

  observed_after_stdin="$(printf '%s\n' "$clean" | section_count '--- post-input ---')"
  observed_after_stdin="$(printf '%s' "${observed_after_stdin:-0}" | tr -cd '0-9')"
  observed_after_stdin="${observed_after_stdin:-0}"

  if printf '%s\n' "$clean" | expect_hello_output ||
     printf '%s\n' "$clean" | has_uniproton_response_markers ||
     [ "${observed_after_stdin:-0}" -gt 0 ]; then
    echo -e "$PASS"
    return 0
  fi

  echo -e "$FAIL"
  echo "  post-input: ${observed_after_stdin:-0}, Output: $(echo "$clean" | head -5)"
  return 1
}

test_hello_attach_input_path() {
  echo -n "Test 6: ctr attach delayed input path stays observable... "
  local out
  local clean
  local cid="hello-attach-$RANDOM"
  local observed_after_stdin
  ensure_containerd || true
  sleep 2
  cleanup_container_id "$cid"
  out="$(remote "
    ctr container create --runtime io.containerd.mica.v2 $IMAGE $cid >/dev/null 2>&1
    ctr task start -d $cid >/dev/null 2>&1
    sleep 3
    (sleep ${UNIPROTON_INTERACTION_WAIT_SECS}; printf \"\\nhelp\\nuname\\n\"; sleep 10) | timeout 35 ctr task attach $cid 2>&1 || true
    echo '--- post-input ---'
    grep -c 'Observed output after stdin for $cid' /var/log/mica/mica-runtime.log || true
    ctr task kill -s 9 $cid 2>/dev/null || true
    ctr task delete $cid 2>/dev/null || true
    ctr container delete $cid 2>/dev/null || true
  " 2>&1)"
  clean="$(printf '%s\n' "$out" | sanitize_command_output)"
  observed_after_stdin="$(printf '%s\n' "$clean" | section_count '--- post-input ---')"
  observed_after_stdin="$(printf '%s' "${observed_after_stdin:-0}" | tr -cd '0-9')"
  observed_after_stdin="${observed_after_stdin:-0}"

  if { printf '%s\n' "$clean" | expect_hello_output &&
       printf '%s\n' "$clean" | has_uniproton_response_markers; } ||
     [ "${observed_after_stdin:-0}" -gt 0 ]; then
    echo -e "$PASS"
    return 0
  fi

  echo -e "$FAIL"
  echo "  post-input: ${observed_after_stdin:-0}, Output: $(echo "$clean" | head -5)"
  return 1
}

test_hello_cleanup() {
  echo -n "Test 7: lifecycle cleanup... "
  local out
  cleanup_container_id "t5"
  out="$(remote "
    ctr container create --runtime io.containerd.mica.v2 $IMAGE t5 >/dev/null 2>&1
    ctr task start -d t5 >/dev/null 2>&1
    sleep 2
    echo '--- run-state ---'
    ctr task ls | awk '$1 == "t5" {print $3; found=1} END {if (!found) print "GONE"}' 
    ctr task kill -s 9 t5 >/dev/null 2>&1 || true
    ctr task delete t5 >/dev/null 2>&1 || true
    ctr container delete t5 >/dev/null 2>&1 || true
    sleep 1
    ctr task ls | grep -c t5 || true
    ctr container ls | grep -c t5 || true
  " 2>/dev/null)"

  # The cleanup verdict is only meaningful if the task was actually
  # running first; a failed create/start left 0 residue and passed vacuously.
  run_state="$(sed -n '/--- run-state ---/{n;p}' "$out" 2>/dev/null | tr -d '[:space:]')"
  if [ "$run_state" = "RUNNING" ] &&
     [ "$(echo "$out" | grep -oE '[0-9]+' | tail -2 | tr '\n' ' ')" = "0 0 " ]; then
    echo -e "$PASS"
    return 0
  fi

  echo -e "$FAIL"
  echo "  Output: $(echo "$out" | tail -4)"
  return 1
}

test_hello_log_cleanliness() {
  echo -n "Test 8: log cleanliness... "
  local count
  cleanup_container_id "t6"
  count="$(remote "
    ctr container create --runtime io.containerd.mica.v2 $IMAGE t6 >/dev/null 2>&1
    ctr task start -d t6 >/dev/null 2>&1
    sleep 3
    ctr task ls | awk '$1 == "t6" {print $3; found=1} END {if (!found) print "GONE"}' 
    echo help | timeout 8 ctr task attach t6 >/dev/null 2>&1 || true
    sleep 1
    tail -100 /var/log/mica/mica-runtime.log | grep -c 'stdin FIFO read' || echo 0
    ctr task kill -s 9 t6 2>/dev/null || true
    ctr task delete t6 2>/dev/null || true
    ctr container delete t6 2>/dev/null || true
  " 2>/dev/null)"

  run_state="$(echo "$count" | head -1 | tr -d '[:space:]')"
  count="$(echo "$count" | tr -d '\n ' | grep -oE '[0-9]+' | head -1 || echo 0)"

  # GONE/STOPPED means the container never ran: the log-count check alone
  # passed vacuously in that case.
  if [ "$run_state" = "RUNNING" ] && [ "${count:-0}" -lt 5 ]; then
    echo -e "$PASS (${count} logs)"
    return 0
  fi

  echo -e "$FAIL (state=${run_state:-<none>}, ${count} logs)"
  return 1
}

# Select cases: MICRUN_IO_CASES=1,10 or args `1 10` / `auto-close`.
# Empty means the full suite.
MICRUN_IO_CASES="${MICRUN_IO_CASES:-}"
while [ $# -gt 0 ]; do
  case "$1" in
    --case|--cases)
      if [ $# -lt 2 ]; then
        echo "test_suite.sh: $1 requires a value" >&2
        exit 2
      fi
      MICRUN_IO_CASES="${MICRUN_IO_CASES:+$MICRUN_IO_CASES,}$2"
      shift 2
      ;;
    --help|-h)
      echo "Usage: test_suite.sh [case ...]"
      echo "  cases: 0-14 or native,ctr-bg,tty,attach-detach,ux,notty,notty-xen,"
      echo "         nerdctl-lifecycle,ctr-lifecycle,diagnostics,multi,logs,"
      echo "         exit,auto-close,manual-exit"
      echo "  MICRUN_IO_CASES=1,10  same as args"
      exit 0
      ;;
    *)
      MICRUN_IO_CASES="${MICRUN_IO_CASES:+$MICRUN_IO_CASES,}$1"
      shift
      ;;
  esac
done

echo "╔════════════════════════════════════════════════════════════╗"
echo "║              MicRun IO Test Suite                          ║"
echo "╚════════════════════════════════════════════════════════════╝"
echo ""
echo "Remote: $REMOTE"
echo "Image: $IMAGE"

io_normalize_case() {
  case "$1" in
    native|0) echo 0 ;;
    ctr-bg|background|1) echo 1 ;;
    nerdctl-tty|tty|2) echo 2 ;;
    attach-detach|3) echo 3 ;;
    ux|ux-matrix|4) echo 4 ;;
    notty|5) echo 5 ;;
    notty-xen|6) echo 6 ;;
    nerdctl-lifecycle|7) echo 7 ;;
    ctr-lifecycle|8) echo 8 ;;
    diagnostics|9) echo 9 ;;
    multi|10) echo 10 ;;
    logs|11) echo 11 ;;
    exit|12) echo 12 ;;
    auto-close|13) echo 13 ;;
    manual-exit|14) echo 14 ;;
    *) echo "$1" ;;
  esac
}

io_case_selected() {
  local want="$1"
  local raw item
  [ -z "${MICRUN_IO_CASES}" ] && return 0
  IFS=', ' read -r -a raw <<< "${MICRUN_IO_CASES}"
  for item in "${raw[@]}"; do
    item="$(echo "$item" | tr -d '[:space:]')"
    [ -z "$item" ] && continue
    [ "$(io_normalize_case "$item")" = "$want" ] && return 0
  done
  return 1
}

io_run() {
  local id="$1"
  local fn="$2"
  io_case_selected "$id" || return 0
  if "$fn"; then
    pass=$((pass + 1))
  else
    rc=$?
    if [ "$rc" -eq 2 ]; then
      skip=$((skip + 1))
    else
      fail=$((fail + 1))
    fi
  fi
}

ensure_containerd || true
if [ -n "${MICRUN_IO_CASES}" ]; then
  echo "Cases: ${MICRUN_IO_CASES}"
  cleanup_between_tests
else
  cleanup_all
fi

profile="$(detect_image_profile)"
echo "Profile: ${profile}"
echo ""

pass=0
fail=0
skip=0

if image_profile_is_shell_family; then
  io_run 0 test_native_mica_shell_preflight
  io_run 1 test_shell_ctr_background
  io_run 2 test_shell_nerdctl_tty
  io_run 3 test_shell_nerdctl_attach_detach
  io_run 4 test_shell_nerdctl_tty_ux_matrix
  io_run 5 test_shell_nerdctl_notty
  io_run 6 test_shell_notty_xen_cleanup
  io_run 7 test_shell_nerdctl_create_start_stop
  io_run 8 test_shell_ctr_lifecycle_status
  io_run 9 test_shell_user_diagnostics
  io_run 10 test_shell_multiple_commands
  io_run 11 test_shell_log_cleanliness
  io_run 12 test_shell_exit_command
  io_run 13 test_shell_ctr_foreground_auto_close
  io_run 14 test_shell_ctr_foreground_manual_exit
elif image_profile_is "hello"; then
  io_run 0 test_native_mica_shell_preflight
  io_run 1 test_hello_ctr_background_running
  io_run 2 test_hello_ctr_foreground_output
  io_run 3 test_hello_nerdctl_create_start
  io_run 4 test_hello_nerdctl_detached_lifecycle
  io_run 5 test_hello_nerdctl_notty_output
  io_run 6 test_hello_attach_input_path
  io_run 7 test_hello_cleanup
  io_run 8 test_hello_log_cleanliness
else
  echo -e "${FAIL}"
  echo "Unknown image profile. Probe output:"
  echo "$PROFILE_PROBE_OUTPUT" | head -40
  fail=$((fail + 1))
fi

echo ""
echo "╔════════════════════════════════════════════════════════════╗"
echo "║                      Results                              ║"
echo "╠════════════════════════════════════════════════════════════╣"
printf "║  Passed: %-3d Failed: %-3d Skipped: %-3d                     ║\n" "$pass" "$fail" "$skip"
echo "╚════════════════════════════════════════════════════════════╝"

if [ -n "${MICRUN_IO_CASES}" ]; then
  cleanup_between_tests
else
  cleanup_all
fi

if [ "$fail" -ne 0 ]; then
  exit "$fail"
fi

exit 0
