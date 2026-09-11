#!/usr/bin/env bash
set -euo pipefail
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
bash -n "$root/scripts/benchmark-install.sh"
for eco in pypi npm; do
  result=$("$root/scripts/benchmark-install.sh" --ecosystem "$eco" --runs 2 --depsilo-url https://registry.example.test --dry-run)
  python3 - "$result" "$eco" <<'PY'
import json, sys
report=json.loads(sys.argv[1])
assert report['status'] == 'NOT_RUN'
assert report['ecosystem'] == sys.argv[2]
assert report['runs'] == 2
assert report['scenarios'] == ['direct_cold','depsilo_client_cold','depsilo_hot_client_cold','client_warm_direct','client_warm_depsilo']
PY
done
python3 - "$root/scripts/benchmark-install.sh" <<'PY'
import collections
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile

script = sys.argv[1]
fake_client = '''#!/usr/bin/env python3
import json, os, shlex, sys
from pathlib import Path

args = sys.argv[1:]
trace = Path(os.environ['BENCHMARK_TRACE'])
failure = os.environ.get('BENCHMARK_FAILURE', '')
tool = Path(sys.argv[0]).name
if args == ['--version']:
    print('fixture Docker version')
    sys.exit(0)
if args[:2] == ['image', 'inspect']:
    print('[{"Id":"sha256:fixture","RepoDigests":["fixture@sha256:fixture"]}]')
    sys.exit(0)
cache = None
command = args
image = None
if tool == 'docker':
    assert args[0] == 'run', args
    i = 1
    while args[i].startswith('-'):
        if args[i] in ('-v', '--volume'):
            cache = Path(args[i+1].rsplit(':', 1)[0])
            i += 2
        elif args[i] in ('-w', '--workdir', '-e', '--env', '--add-host'):
            i += 2
        else:
            i += 1
    image, command = args[i], args[i+1:]
    if '--version' in command:
        print('fixture package manager version')
        sys.exit(0)
tokens = shlex.split(command[-1]) if command[:2] == ['sh', '-c'] else command
install = 'install' in tokens
record = {'tool': tool, 'kind': 'install' if install else 'probe',
          'args': args, 'command': tokens, 'image': image}
previous = [json.loads(line) for line in trace.read_text().splitlines()] if trace.exists() else []
code = 0
if install:
    assert cache is not None and cache.is_dir(), args
    record.update(cache=str(cache), warm=any(cache.iterdir()))
    first = not any(item.get('cache') == str(cache) for item in previous)
    prewarm = first and ('client_warm' in cache.name or 'client-warm' in cache.name or 'server-warm' in cache.name)
    code = 17 if failure == 'prewarm' and prewarm else (41 if failure == 'install' else 0)
    if code == 0:
        (cache / 'fixture-cache-entry').write_text('cached')
elif failure == 'probe':
    code = 23
record['status'] = code
with trace.open('a') as stream:
    stream.write(json.dumps(record) + '\\n')
if not os.environ.get('BENCHMARK_EMPTY_OUTPUT'):
    print('fixture %s %s' % (record['kind'], 'failure' if code else 'success'))
sys.exit(code)
'''

with tempfile.TemporaryDirectory(prefix='depsilo benchmark test ') as directory:
    root = Path(directory)
    bin_dir = root / 'bin'
    bin_dir.mkdir()
    for name in ('docker', 'curl'):
        executable = bin_dir / name
        executable.write_text(fake_client)
        executable.chmod(0o755)
    env = dict(os.environ, PATH=str(bin_dir) + os.pathsep + os.environ['PATH'], TMPDIR=str(root))

    def run_case(name, eco='pypi', url='https://registry.example.test', extra=(), failure='', empty=False):
        case = root / name
        case.mkdir()
        trace, output = case / 'trace.jsonl', case / 'results.json'
        result = subprocess.run(['bash', script, '--ecosystem', eco, '--runs', '2',
                                 '--depsilo-url', url, '--out', str(output), *extra],
                                env=dict(env, BENCHMARK_TRACE=str(trace), BENCHMARK_FAILURE=failure,
                                         BENCHMARK_EMPTY_OUTPUT='1' if empty else ''),
                                text=True, capture_output=True, timeout=30)
        calls = [json.loads(line) for line in trace.read_text().splitlines()] if trace.exists() else []
        report = json.loads(output.read_text()) if output.exists() else None
        return result, calls, report

    for eco in ('pypi', 'npm'):
        result, calls, report = run_case(eco, eco=eco)
        assert result.returncode == 0, result.stderr
        assert report['status'] == 'complete' and len(report['samples']) == 10, report
        assert set(collections.Counter(row['scenario'] for row in report['samples']).values()) == {2}
        assert all(row['successful'] == 2 for row in report['summary']), report['summary']
        installs = [call for call in calls if call['kind'] == 'install']
        assert len(installs) == 16, installs
        cold = set()
        for offset in (0, 8):
            batch = installs[offset:offset+8]
            for prewarm, timed in ((batch[4], batch[5]), (batch[6], batch[7])):
                assert prewarm['cache'] == timed['cache'], ('warm cache mismatch', prewarm['cache'], timed['cache'])
                assert not prewarm['warm'] and timed['warm'], (prewarm, timed)
            for call in (batch[0], batch[1], batch[3]):
                assert not call['warm'] and call['cache'] not in cold, call
                cold.add(call['cache'])
            assert batch[2]['cache'] != batch[3]['cache'], batch
            for i, call in enumerate(batch):
                expected = ('https://pypi.org/simple/' if eco == 'pypi' else 'https://registry.npmjs.org/') if i in (0, 4, 5) else 'https://registry.example.test/' + ('pypi/simple/' if eco == 'pypi' else 'npm/')
                option = '--index-url' if eco == 'pypi' else '--registry'
                assert call['command'][call['command'].index(option)+1] == expected, call
                assert '--trusted-host' not in call['command'] and 'sh' not in call['command'], call
                cache_option = '--cache-dir' if eco == 'pypi' else '--cache'
                assert call['command'][call['command'].index(cache_option)+1] == ('/root/.cache/pip' if eco == 'pypi' else '/root/.npm'), call
        probes = [call for call in calls if call['kind'] == 'probe']
        assert len(probes) == 6 and all(call['tool'] == 'docker' for call in probes), probes
        expected_probe = ('/pypi/simple/requests/' if eco == 'pypi' else '/npm/lodash')
        assert all(any(expected_probe in token for token in call['command']) for call in probes), probes
        assert all('--add-host=host.docker.internal:host-gateway' in call['args'] for call in installs + probes)
        assert all(call['image'] == installs[0]['image'] for call in probes)
        assert report['environment']['client_image'] == installs[0]['image'], report

    result, calls, report = run_case('local-http', url='http://host.docker.internal:23333', extra=('--allow-local-http',))
    assert result.returncode == 0, result.stderr
    for call in (call for call in calls if call['kind'] == 'install'):
        command = call['command']
        endpoint = command[command.index('--index-url')+1]
        if endpoint.startswith('http:'):
            assert command[command.index('--trusted-host')+1] == 'host.docker.internal:23333', command
        else:
            assert '--trusted-host' not in command, command
    assert report['environment']['http_trusted_host'] == 'host.docker.internal:23333'

    for name, url, extra in [
        ('http-no-consent', 'http://host.docker.internal:23333', ()),
        ('public-http', 'http://registry.example.test', ('--allow-local-http',)),
        ('credentials', 'https://user:password@registry.example.test', ()),
        ('bad-scheme', 'ftp://registry.example.test', ()),
    ]:
        result, calls, report = run_case(name, url=url, extra=extra)
        assert result.returncode == 2 and not calls and report is None, (name, result, calls)

    for failure in ('prewarm', 'probe', 'install'):
        result, calls, report = run_case(failure, failure=failure)
        assert result.returncode == 1 and len(report['samples']) == 10, (result, report)
        assert report['status'] == ('failed' if failure == 'install' else 'partial'), report
        key = {'prewarm': 'prewarm_status', 'probe': 'probe_status', 'install': 'install_status'}[failure]
        affected = [row for row in report['samples'] if row[key] not in (None, 0)]
        assert len(affected) == (10 if failure == 'install' else 6), report
        for row in affected:
            assert row['status'] != 0 and 'failure' in row['error'], row
            log = Path(report['evidence_dir']) / row[failure + '_log']
            assert log.is_file() and 'failure' in log.read_text(), log

    result, calls, report = run_case('empty-output', empty=True)
    assert result.returncode == 0 and report['status'] == 'complete', (result, report)
print('benchmark command, cache isolation, HTTP, and failure contracts passed (fake clients only)')
PY
