#!/usr/bin/env bash
set -euo pipefail

eco=""
runs=3
out="benchmark-results.json"
proxy="${DEPSILO_URL:-http://host.docker.internal:23333}"
dry=false
allow_local_http=false
while [[ $# -gt 0 ]]; do
  case "$1" in
    --ecosystem) eco=${2:?missing ecosystem}; shift 2 ;;
    --runs) runs=${2:?missing runs}; shift 2 ;;
    --out) out=${2:?missing output}; shift 2 ;;
    --depsilo-url) proxy=${2:?missing URL}; shift 2 ;;
    --allow-local-http) allow_local_http=true; shift ;;
    --dry-run) dry=true; shift ;;
    -h|--help) echo 'usage: benchmark-install.sh --ecosystem pypi|npm [--runs N] [--out FILE] [--depsilo-url URL] [--allow-local-http] [--dry-run]'; exit 0 ;;
    *) echo "unknown argument: $1" >&2; exit 2 ;;
  esac
done
[[ "$eco" == pypi || "$eco" == npm ]] || { echo '--ecosystem pypi or npm is required' >&2; exit 2; }
[[ "$runs" =~ ^[1-9][0-9]*$ && "$runs" -le 20 ]] || { echo '--runs must be 1..20' >&2; exit 2; }
command -v python3 >/dev/null || { echo 'python3 is required' >&2; exit 2; }
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
trusted_host=$(python3 - "$proxy" "$allow_local_http" <<'PY'
import ipaddress, sys
from urllib.parse import urlsplit

try:
    raw, allow_http = sys.argv[1:]
    url = urlsplit(raw)
    if (url.scheme not in ('https', 'http') or not url.hostname or
            url.username is not None or url.password is not None or url.query or url.fragment or
            any(char.isspace() or ord(char) < 32 or ord(char) == 127 for char in raw)):
        raise ValueError('--depsilo-url must be an HTTP(S) URL without credentials, query, fragment, or whitespace')
    port = url.port if url.port is not None else (443 if url.scheme == 'https' else 80)
    if not 1 <= port <= 65535:
        raise ValueError('invalid endpoint port')
    if url.scheme == 'http':
        if allow_http != 'true':
            raise ValueError('use HTTPS, or --allow-local-http for an explicit local benchmark endpoint')
        host = url.hostname
        local = host in ('localhost', 'host.docker.internal')
        try:
            address = ipaddress.ip_address(host)
            local = address.is_loopback or any(address in ipaddress.ip_network(network)
                      for network in ('10.0.0.0/8', '172.16.0.0/12', '192.168.0.0/16', 'fc00::/7'))
        except ValueError:
            pass
        if not local:
            raise ValueError('--allow-local-http accepts only loopback, private IPs, or host.docker.internal')
        # Include even the default port: trust must not extend to other ports.
        print(f'[{host}]:{port}' if ':' in host else f'{host}:{port}')
except ValueError as error:
    print(error, file=sys.stderr)
    sys.exit(2)
PY
) || exit 2
command -v docker >/dev/null || { echo 'docker is required' >&2; exit 2; }
umask 077
work=$(mktemp -d "${TMPDIR:-/tmp}/depsilo-benchmark.XXXXXX")
trap 'rm -r "$work"' EXIT
# Retain command output next to the report; only disposable client caches are removed.
evidence=$(mktemp -d "${out}.logs.XXXXXX")
raw="$evidence/raw.tsv"; : > "$raw"
docker_run=(docker run --rm --add-host=host.docker.internal:host-gateway)
capture() {
  local log=$1
  shift
  if "$@" >"$evidence/$log" 2>&1; then command_status=0; else command_status=$?; fi
}
install() {
  local endpoint=$1 cache=$2 log=$3 start end
  local trust_args=()
  if [[ "$endpoint" == "$proxy_endpoint" && -n "$trusted_host" ]]; then
    trust_args=(--trusted-host "$trusted_host")
  fi
  mkdir -p "$cache"; start=$(date +%s%N)
  if [[ "$eco" == pypi ]]; then
    capture "$log" "${docker_run[@]}" -v "$cache:$mount" "$image" \
      python -m pip --isolated install --disable-pip-version-check --no-input \
      --cache-dir "$mount" --index-url "$endpoint" "${trust_args[@]}" "$package"
  else
    capture "$log" "${docker_run[@]}" -v "$cache:$mount" --workdir /tmp/bench "$image" \
      npm install --cache "$mount" --ignore-scripts --no-audit --no-fund --registry "$endpoint" "$package"
  fi
  end=$(date +%s%N)
  install_nanos=$((end-start))
}
probe() {
  # Use the client image and network, with normal TLS verification. A host curl
  # cannot prove that host.docker.internal is reachable inside this container.
  if [[ "$eco" == pypi ]]; then
    capture "$1" "${docker_run[@]}" "$image" python -c '
import sys, urllib.request
from urllib.parse import urlsplit
url = sys.argv[1]
with urllib.request.urlopen(url, timeout=20) as response:
    assert urlsplit(response.geturl()).netloc == urlsplit(url).netloc, "probe redirected to a different endpoint"
    assert urlsplit(response.geturl()).scheme == urlsplit(url).scheme, "probe changed transport"
    print("HTTP", response.status)
' "${proxy_endpoint}requests/"
  else
    capture "$1" "${docker_run[@]}" "$image" node -e '
fetch(process.argv[1], {signal: AbortSignal.timeout(20000), redirect: "error"})
  .then(async response => {
    if (!response.ok) throw new Error(`HTTP ${response.status}`);
    console.log(`HTTP ${response.status}`);
    await response.body?.cancel();
  }).catch(error => { console.error(error); process.exitCode = 1; });
' "${proxy_endpoint}lodash"
  fi
}

# Resolve the client image before timing so the first sample does not include a pull.
capture docker-version.log docker --version
if [[ "$eco" == pypi ]]; then
  capture client-version.log "${docker_run[@]}" "$image" python -m pip --version
else
  capture client-version.log "${docker_run[@]}" "$image" npm --version
fi
capture image.json docker image inspect "$image"
for ((run=1; run<=runs; run++)); do
  for scenario in "${scenarios[@]}"; do
    cache="$work/$scenario-$run"
    prewarm_cache=""; prewarm_log=""; probe_log=""
    prewarm_status=""; probe_status=""
    case "$scenario" in
      direct_cold) endpoint="$direct_endpoint" ;;
      depsilo_client_cold) endpoint="$proxy_endpoint" ;;
      depsilo_hot_client_cold) endpoint="$proxy_endpoint"; prewarm_cache="$work/server-warm-$run" ;;
      client_warm_direct) endpoint="$direct_endpoint"; prewarm_cache="$cache" ;;
      client_warm_depsilo) endpoint="$proxy_endpoint"; prewarm_cache="$cache" ;;
    esac
    if [[ "$endpoint" == "$proxy_endpoint" ]]; then
      probe_log="$scenario-$run-probe.log"
      probe "$probe_log"
      probe_status=$command_status
    fi
    if [[ -n "$prewarm_cache" ]]; then
      prewarm_log="$scenario-$run-prewarm.log"
      install "$endpoint" "$prewarm_cache" "$prewarm_log"
      prewarm_status=$command_status
    fi
    install_log="$scenario-$run-install.log"
    install "$endpoint" "$cache" "$install_log"
    code=$command_status
    if [[ "${prewarm_status:-0}" != 0 || "${probe_status:-0}" != 0 ]]; then
      code=125
    fi
    printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n' \
      "$run" "$scenario" "$code" "$command_status" "$prewarm_status" "$probe_status" \
      "$install_nanos" "$install_log" "$prewarm_log" "$probe_log" >> "$raw"
  done
done
python3 - "$evidence" "$out" "$eco" "$package" "$runs" "$image" "$direct_endpoint" "$proxy_endpoint" "$trusted_host" <<'PY'
import json,pathlib,platform,statistics,sys
evidence,out,eco,pkg,runs,image,direct,proxy,trusted=sys.argv[1:]
evidence=pathlib.Path(evidence).resolve()
rows=[]
for line in (evidence/'raw.tsv').read_text().splitlines():
    run,scenario,code,install,prewarm,probe,nanos,install_log,prewarm_log,probe_log=line.split('\t')
    row={'run':int(run),'sample':1,'scenario':scenario,'status':int(code),'wall_seconds':int(nanos)/1e9}
    errors=[]
    for phase,status,log in [('install',install,install_log),('prewarm',prewarm,prewarm_log),('probe',probe,probe_log)]:
        row[phase+'_status']=int(status) if status else None
        row[phase+'_log']=log or None
        if status and int(status):
            output=(evidence/log).read_text(errors='replace').strip()
            errors.append(f'{phase} failed (exit {status}): {output[-1000:] or "no command output"}')
    row['error']='; '.join(errors)
    rows.append(row)
summary=[]
for scenario in sorted({r['scenario'] for r in rows}):
    vals=[r['wall_seconds'] for r in rows if r['scenario']==scenario and r['status']==0]
    summary.append({'scenario':scenario,'samples':sum(r['scenario']==scenario for r in rows),'successful':len(vals),'failed':sum(r['scenario']==scenario and r['status'] != 0 for r in rows),'median_wall_seconds':statistics.median(vals) if vals else None,'min_wall_seconds':min(vals) if vals else None,'max_wall_seconds':max(vals) if vals else None})
status='complete' if all(item['successful'] == int(runs) for item in summary) else ('partial' if any(item['successful'] for item in summary) else 'failed')
try:
    image_identity=json.loads((evidence/'image.json').read_text())
except json.JSONDecodeError:
    image_identity=None
environment={'host':platform.platform(),'client_image':image,'image_identity':image_identity,
             'docker_version':(evidence/'docker-version.log').read_text(errors='replace').strip(),
             'client_version':(evidence/'client-version.log').read_text(errors='replace').strip(),
             'direct_endpoint':direct,'depsilo_endpoint':proxy,'http_trusted_host':trusted or None}
pathlib.Path(out).write_text(json.dumps({'status':status,'ecosystem':eco,'package':pkg,'runs':int(runs),
    'environment':environment,'evidence_dir':str(evidence),'samples':rows,'summary':summary,
    'limitations':['External network and registry state affect results; server storage is externally managed.',
                   'Wall time includes container startup and install; image resolution, probe, and prewarm are excluded.',
                   'Client-hot samples reuse their successful prewarm cache; client-cold samples use fresh directories.',
                   'No server-cold state reset or guarantee of server hotness after prewarm is claimed.',
                   'The top-level package is fixed, but transitive dependencies are not locked.',
                   'No speedup or savings claim is computed.']},indent=2)+'\n')
print(f'Benchmark status: {status}; report: {out}; raw output: {evidence}')
if status != 'complete':
    raise SystemExit(1)
PY
