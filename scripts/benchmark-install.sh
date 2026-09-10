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
if [[ "$eco" == pypi ]]; then
  image='python:3.11-slim'; package='requests==2.32.3'; mount='/root/.cache/pip'
  direct_endpoint='https://pypi.org/simple/'
  proxy_endpoint="${proxy%/}/pypi/simple/"
else
  image='node:22-alpine'; package='lodash@4.17.21'; mount='/root/.npm'
  direct_endpoint='https://registry.npmjs.org/'
  proxy_endpoint="${proxy%/}/npm/"
fi
scenarios=(direct_cold depsilo_client_cold depsilo_hot_client_cold client_warm_direct client_warm_depsilo)
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
  local endpoint=$1 cache=$2 start end code log message
  log=$(mktemp "$work/install.XXXXXX")
  mkdir -p "$cache"; start=$(date +%s%N)
  if [[ "$eco" == pypi ]]; then
    if docker run --rm --add-host=host.docker.internal:host-gateway -v "$cache:$mount" "$image" sh -c "pip install --disable-pip-version-check --no-input --index-url '$endpoint' '$package'" >"$log" 2>&1; then code=0; else code=$?; fi
  else
    if docker run --rm --add-host=host.docker.internal:host-gateway -v "$cache:$mount" "$image" sh -c "mkdir -p /tmp/bench && cd /tmp/bench && npm install --ignore-scripts --no-audit --no-fund --registry '$endpoint' '$package'" >"$log" 2>&1; then code=0; else code=$?; fi
  fi
  end=$(date +%s%N)
  message=$(tr '\n' ' ' <"$log" | cut -c1-500)
  rm -f "$log"
  printf '%s\t%s\t%s\n' "$code" "$((end-start))" "$message"
}
for ((run=1; run<=runs; run++)); do
  for scenario in "${scenarios[@]}"; do
    cache="$work/$scenario-$run"
    prewarm_failed=false
    case "$scenario" in
      direct_cold) endpoint="$direct_endpoint" ;;
      depsilo_client_cold) endpoint="$proxy_endpoint" ;;
      depsilo_hot_client_cold)
        endpoint="$proxy_endpoint"
        IFS=$'\t' read -r warm_code _ warm_message < <(install "$endpoint" "$work/server-warm-$run")
        if [[ "$warm_code" != 0 ]]; then
          echo "Depsilo prewarm failed: $warm_message" >&2
          prewarm_failed=true
        fi
        ;;
      client_warm_direct) endpoint="$direct_endpoint"; IFS=$'\t' read -r warm_code _ warm_message < <(install "$endpoint" "$work/client-warm-direct-$run"); [[ "$warm_code" == 0 ]] || prewarm_failed=true ;;
      client_warm_depsilo) endpoint="$proxy_endpoint"; IFS=$'\t' read -r warm_code _ warm_message < <(install "$endpoint" "$work/client-warm-depsilo-$run"); [[ "$warm_code" == 0 ]] || prewarm_failed=true ;;
    esac
    endpoint_failed=false
    if [[ "$endpoint" == "$proxy_endpoint" ]]; then
      command -v curl >/dev/null || { echo 'curl is required for Depsilo endpoint checks' >&2; exit 2; }
      probe="${proxy_endpoint%/}/$( [[ "$eco" == pypi ]] && echo requests/ || echo lodash )"
      if ! curl --fail --silent --show-error --max-time 20 "$probe" >/dev/null; then
        echo "Depsilo endpoint probe failed: $probe" >&2
        endpoint_failed=true
      fi
    fi
    IFS=$'\t' read -r code nanos message < <(install "$endpoint" "$cache")
    if [[ "$prewarm_failed" == true || "$endpoint_failed" == true ]]; then
      code=125
      message="precondition failed: ${message:-no command output}"
    fi
    printf '%s\t%s\t%s\t%s\t%s\t%s\n' "$run" 1 "$scenario" "$code" "$nanos" "$message" >> "$raw"
  done
done
python3 - "$raw" "$out" "$eco" "$package" "$runs" <<'PY'
import json,pathlib,statistics,sys
raw,out,eco,pkg,runs=sys.argv[1:]; rows=[]
for line in pathlib.Path(raw).read_text().splitlines():
    run,sample,scenario,code,nanos,message=line.split('\t',5); rows.append({'run':int(run),'sample':int(sample),'scenario':scenario,'status':int(code),'wall_seconds':int(nanos)/1e9,'error':message if int(code) else ''})
summary=[]
for scenario in sorted({r['scenario'] for r in rows}):
    vals=[r['wall_seconds'] for r in rows if r['scenario']==scenario and r['status']==0]
    summary.append({'scenario':scenario,'samples':sum(r['scenario']==scenario for r in rows),'successful':len(vals),'failed':sum(r['scenario']==scenario and r['status'] != 0 for r in rows),'median_wall_seconds':statistics.median(vals) if vals else None,'min_wall_seconds':min(vals) if vals else None,'max_wall_seconds':max(vals) if vals else None})
status='complete' if all(item['successful'] == int(runs) for item in summary) else ('partial' if any(item['successful'] for item in summary) else 'failed')
pathlib.Path(out).write_text(json.dumps({'status':status,'ecosystem':eco,'package':pkg,'runs':int(runs),'samples':rows,'summary':summary,'limitations':['External network and registry state affect results.','Depsilo hot client-cold prewarms the service but uses a fresh client cache for each timed sample.','A fully server-cold sample requires a dedicated temporary Depsilo instance and is not claimed here.','No speedup or savings claim is computed.']},indent=2)+'\n')
if status != 'complete':
    raise SystemExit(1)
PY
echo "Benchmark results written locally: $out"
