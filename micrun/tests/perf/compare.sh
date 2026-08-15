#!/bin/bash
# Compare a perf measurement (tests/perf/measure.sh output) against the
# committed baseline. Policy (docs/internals/testing.md §1.6):
#   - median drift > 25% on any wall metric  -> FAIL (regression suspected)
#   - median drift > 10%                     -> WARN (re-run to confirm)
# Stage-line medians are reported informationally; they lack historical
# depth until more baselines accumulate.
#
# Usage: tests/perf/compare.sh [result.json]   (default /tmp/micrun-perf-result.json)
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
RESULT="${1:-/tmp/micrun-perf-result.json}"
BASELINE="$SCRIPT_DIR/baseline.json"

python3 - "$BASELINE" "$RESULT" <<'PY'
import json, re, statistics, sys

base_path, res_path = sys.argv[1:3]
base = json.load(open(base_path))
res = json.load(open(res_path))

fail = warn = 0
print(f"{'metric':<18}{'baseline':>10}{'now':>10}{'drift':>8}  verdict")
for m, b in base["wall"].items():
    r = res["wall"].get(m)
    if not r:
        print(f"{m:<18}{b['median']:>10}{'>':>10}{'-':>8}  MISSING")
        fail += 1
        continue
    drift = (r["median"] - b["median"]) / b["median"] * 100
    verdict = "ok"
    if drift > 25:
        verdict = "FAIL"; fail += 1
    elif drift > 10:
        verdict = "WARN"; warn += 1
    print(f"{m:<18}{b['median']:>10}{r['median']:>10}{drift:>7.1f}%  {verdict}")

def stage_medians(doc):
    out = {}
    for line in doc.get("stages", []):
        m = re.match(r"\s*(\d+) \[PERF\] (\S+) ([0-9.]+[a-zµ]*)", line)
        if m:
            out.setdefault(m.group(2), []).append(m.group(3))
    return out

bs, rs = stage_medians(base), stage_medians(res)
if bs and rs:
    print("\nstage medians (informational):")
    for k in sorted(set(bs) | set(rs)):
        print(f"  {k:<40} base n={len(bs.get(k, []))} now n={len(rs.get(k, []))}")

print(f"\n{fail} FAIL / {warn} WARN")
sys.exit(1 if fail else 0)
PY
