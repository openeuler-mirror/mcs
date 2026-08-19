#!/bin/bash
# High-risk pattern gate. Each rule encodes a defect class that escaped
# review at least once (docs/internals/reliability.md has the full mapping
# from pattern to incident). Rules are conservative: they forbid patterns
# that historically only appeared in defects, so a hit means "explain or
# restructure", not "style preference".
set -u
cd "$(dirname "$0")/.."
fail=0

# P1 — bare task-registry access outside its single choke-point file.
# Incident class: fatal concurrent map read/write (shim crash orphans every
# guest domain). All access must go through runtime_state.go accessors.
while IFS=: read -r file line text; do
    [ -z "${file:-}" ] && continue
    echo "P1 bare containers-map access: $file:$line: $text"
    fail=1
done < <(grep -rn -E '\.containers(\[|= |, )' internal/transport/shimv2/*.go 2>/dev/null \
    | grep -v '_test.go' | grep -v 'runtime_state.go' | grep -v 'taskContainers\|containersDir\|ensureTaskStore' || true)

# P2 — string-typed error classification growing new call sites.
# Incident class: transient failures misclassified as not-found (orphaned
# live domains). isMissingDomainError is quarantined to its two existing
# files; new classification must use typed sentinel errors.
while IFS=: read -r file line text; do
    [ -z "${file:-}" ] && continue
    echo "P2 string-matched error classification: $file:$line: $text"
    fail=1
done < <(grep -rn 'Contains.*"not found"\|Contains.*"does not exist"' internal/ --include='*.go' 2>/dev/null \
    | grep -v '_test.go' \
    | grep -vE 'micad/control.go|sandbox_loader.go' || true)

# P3 — shell tests: blind fixed sleeps right after a mutating guest call
# (create/start) instead of polling. Incident class: startup races that
# surfaced only on slow first boots (lifecycle cases 9/11/12).
while IFS=: read -r file line text; do
    [ -z "${file:-}" ] && continue
    echo "P3 blind sleep after guest run (use guest_ec + poll): $file:$line: $text"
    fail=1
done < <(grep -rn -A2 'guest_sh "ctr run' tests/bin/*.sh 2>/dev/null \
    | grep -E 'sleep [0-9]' | grep -vE 'poll|while' || true)

if [ "$fail" -ne 0 ]; then
    echo ""
    echo "Pattern gate failed — see docs/internals/reliability.md for the fix pattern."
    exit 1
fi
echo "pattern gate: clean"
