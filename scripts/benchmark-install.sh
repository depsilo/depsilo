#!/usr/bin/env bash
set -euo pipefail

eco=""
runs=3
out="benchmark-results.json"
proxy="${DEPSILO_URL:-http://host.docker.internal:23333}"
dry=false
while [[ $# -gt 0 ]]; do
  case "$1" in
    --ecosystem) eco=${2:?missing ecosystem}; shift 2 ;;
    --runs) runs=${2:?missing runs}; shift 2 ;;
    --out) out=${2:?missing output}; shift 2 ;;
    --depsilo-url) proxy=${2:?missing URL}; shift 2 ;;
    --dry-run) dry=true; shift ;;
    -h|--help) echo 'usage: benchmark-install.sh --ecosystem pypi|npm [--runs N] [--out FILE] [--depsilo-url URL] [--dry-run]'; exit 0 ;;
    *) echo "unknown argument: $1" >&2; exit 2 ;;
  esac
done
[[ "$eco" == pypi || "$eco" == npm ]] || { echo '--ecosystem pypi or npm is required' >&2; exit 2; }
[[ "$runs" =~ ^[1-9][0-9]*$ && "$runs" -le 20 ]] || { echo '--runs must be 1..20' >&2; exit 2; }
if [[ "$eco" == pypi ]]; then image='python:3.11-slim'; package='requests==2.32.3'; mount='/root/.cache/pip'; direct='https://pypi.org'; else image='node:22-alpine'; package='lodash@4.17.21'; mount='/root/.npm'; direct='https://registry.npmjs.org'; fi
scenarios=(direct_cold depsilo_cold depsilo_hot client_warm_direct client_warm_depsilo)
if "$dry"; then
  python3 - "$eco" "$package" "$runs" "${scenarios[@]}" <<'PY'
import json,sys
eco,pkg,runs,*scenarios=sys.argv[1:]
print(json.dumps({'status':'NOT_RUN','ecosystem':eco,'package':pkg,'runs':int(runs),'scenarios':scenarios,'reason':'dry run; no network or Docker client executed'}))
PY
  exit 0
fi
command -v docker >/dev/null || { echo 'docker is required' >&2; exit 2; }
command -v python3 >/dev/null || { echo 'python3 is required' >&2; exit 2; }
work=$(mktemp -d "${TMPDIR:-/tmp}/depsilo-benchmark.XXXXXX")
trap 'rm -r "$work"' EXIT
raw="$work/raw.tsv"; : > "$raw"
install() {
  local origin=$1 cache=$2 start end code
  mkdir -p "$cache"; start=$(date +%s%N)
  if [[ "$eco" == pypi ]]; then
    if docker run --rm --add-host=host.docker.internal:host-gateway -v "$cache:$mount" "$image" sh -c "pip install --disable-pip-version-check --no-input --index-url '$origin/pypi/simple/' --trusted-host '${origin#*://}' '$package' >/dev/null" >/dev/null 2>&1; then code=0; else code=$?; fi
  else
    if docker run --rm --add-host=host.docker.internal:host-gateway -v "$cache:$mount" "$image" sh -c "mkdir -p /tmp/bench && cd /tmp/bench && npm install --ignore-scripts --no-audit --no-fund --registry '$origin/npm/' '$package' >/dev/null" >/dev/null 2>&1; then code=0; else code=$?; fi
  fi
  end=$(date +%s%N); printf '%s\t%s\n' "$code" "$((end-start))"
}
for ((run=1; run<=runs; run++)); do
  direct_cache="$work/direct-$run"; proxy_cache="$work/proxy-$run"; warm_cache="$work/warm-$run"
  for scenario in "${scenarios[@]}"; do
    case "$scenario" in direct_cold) origin="$direct"; cache="$direct_cache";; depsilo_cold) origin="$proxy"; cache="$proxy_cache";; depsilo_hot) install "$proxy" "$work/prewarm-$run" >/dev/null || true; origin="$proxy"; cache="$proxy_cache";; client_warm_direct) origin="$direct"; cache="$warm_cache"; install "$origin" "$cache" >/dev/null || true;; client_warm_depsilo) origin="$proxy"; cache="$warm_cache";; esac
    for sample in $(seq 1 "$runs"); do IFS=$'\t' read -r code nanos < <(install "$origin" "$cache" || true); printf '%s\t%s\t%s\t%s\t%s\n' "$run" "$sample" "$scenario" "$code" "$nanos" >> "$raw"; done
  done
done
python3 - "$raw" "$out" "$eco" "$package" "$runs" <<'PY'
import json,pathlib,statistics,sys
raw,out,eco,pkg,runs=sys.argv[1:]; rows=[]
for line in pathlib.Path(raw).read_text().splitlines():
    run,sample,scenario,code,nanos=line.split('\t'); rows.append({'run':int(run),'sample':int(sample),'scenario':scenario,'status':int(code),'wall_seconds':int(nanos)/1e9})
summary=[]
for scenario in sorted({r['scenario'] for r in rows}):
    vals=[r['wall_seconds'] for r in rows if r['scenario']==scenario and r['status']==0]
    summary.append({'scenario':scenario,'samples':sum(r['scenario']==scenario for r in rows),'successful':len(vals),'median_wall_seconds':statistics.median(vals) if vals else None,'min_wall_seconds':min(vals) if vals else None,'max_wall_seconds':max(vals) if vals else None})
pathlib.Path(out).write_text(json.dumps({'status':'complete','ecosystem':eco,'package':pkg,'runs':int(runs),'samples':rows,'summary':summary,'limitations':['External network and registry state affect results.','Client caches are isolated; run once against a freshly started Depsilo state for a true cold service sample.','Prewarm is excluded from timed output.','No speedup or savings claim is computed.']},indent=2)+'\n')
PY
echo "Benchmark results written locally: $out"
