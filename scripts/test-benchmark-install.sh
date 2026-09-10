#!/usr/bin/env bash
set -euo pipefail
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
bash -n "$root/scripts/benchmark-install.sh"
for eco in pypi npm; do
  result=$("$root/scripts/benchmark-install.sh" --ecosystem "$eco" --runs 2 --dry-run)
  python3 - "$result" "$eco" <<'PY'
import json, sys
report=json.loads(sys.argv[1])
assert report['status'] == 'NOT_RUN'
assert report['ecosystem'] == sys.argv[2]
assert report['runs'] == 2
assert report['scenarios'] == ['direct_cold','depsilo_client_cold','depsilo_hot_client_cold','client_warm_direct','client_warm_depsilo']
PY
done
echo 'benchmark harness dry-run checks passed'
