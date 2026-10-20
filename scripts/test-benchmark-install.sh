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
    sys.exit(19 if failure == 'docker-version' else 0)
if args[:1] == ['pull']:
    print('fixture image pulled')
    sys.exit(19 if failure == 'image-pull' else 0)
if args[:2] == ['image', 'inspect']:
    if failure == 'image-pull':
        sys.exit(1)
    print(json.dumps([{'Id': 'sha256:fixture', 'Os': 'linux', 'Architecture': 'amd64', 'RepoDigests': ['fixture@sha256:immutable'] if failure == 'repo-digest' else []}]))
    sys.exit(0)
cache = None
command = args
image = None
if tool == 'docker':
    assert args[0] == 'run', args
    i = 1
    while args[i].startswith('-'):
        if args[i] in ('-v', '--volume'):
            if args[i+1].endswith(':/root/.cache/pip') or args[i+1].endswith(':/root/.npm'):
                cache = Path(args[i+1].rsplit(':', 1)[0])
            i += 2
        elif args[i] in ('-w', '--workdir', '-e', '--env', '--add-host', '--platform'):
            i += 2
        else:
            i += 1
    image, command = args[i], args[i+1:]
    if '--version' in command:
        print('fixture package manager version')
        sys.exit(19 if failure == 'client-version' else 0)
if command[:2] == ['python', '-c'] and 'python_version' in command[-1] and 'machine' in command[-1]:
    print(json.dumps({'python_version': '3.12' if failure == 'python-version' else '3.11', 'machine': 'x86_64'}))
    sys.exit(0)
tokens = shlex.split(command[-1]) if command[:2] == ['sh', '-c'] else command
install = 'install' in tokens or 'ci' in tokens
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
        assert report['measurement_ready'] is False
        assert report['environment']['client_image'] == 'sha256:fixture'
        assert report['environment']['timer'] == 'monotonic_ns'
        assert all(row['wall_seconds'] > 0 for row in report['samples'])
        assert report['inputs'] and all(len(value) == 64 for value in report['inputs'].values())
        assert all(row['server_cache_state'] == 'uncontrolled' for row in report['samples'] if 'depsilo' in row['scenario'])
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
                if eco == 'npm':
                    assert call['command'][0:2] == ['npm', 'ci'], call
                else:
                    assert all(flag in call['command'] for flag in ('--require-hashes', '--ignore-installed', '--only-binary=:all:', '-r')), call
                cache_option = '--cache-dir' if eco == 'pypi' else '--cache'
                assert call['command'][call['command'].index(cache_option)+1] == ('/root/.cache/pip' if eco == 'pypi' else '/root/.npm'), call
        probes = [call for call in calls if call['kind'] == 'probe']
        assert len(probes) == 6 and all(call['tool'] == 'docker' for call in probes), probes
        expected_probe = '/ready'
        assert all(any(expected_probe in token for token in call['command']) for call in probes), probes
        assert all('--add-host=host.docker.internal:host-gateway' in call['args'] for call in installs + probes)
        assert all(call['image'] == installs[0]['image'] for call in probes)
        assert report['environment']['client_image'] == installs[0]['image'], report

    for channel in ('cpu', 'cu128'):
        result, calls, report = run_case('pytorch-' + channel, extra=('--pytorch-channel', channel))
        assert result.returncode == 0 and report['status'] == 'complete', (result, report)
        assert report['environment']['client_platform'] == 'linux/amd64', report
        assert report['environment']['python_environment']['python_version'] == '3.11', report
        assert report['environment']['pip_socket_timeout_seconds'] == 600, report
        assert 'profile.json' in report['inputs'], report
        for call in (call for call in calls if call['kind'] == 'install'):
            command = call['command']
            endpoint = command[command.index('--index-url') + 1]
            assert command[command.index('--timeout') + 1] == '600', command
            assert endpoint in (f'https://download.pytorch.org/whl/{channel}/', f'https://registry.example.test/pypi-torch/{channel}/simple/'), command
            assert call['args'][call['args'].index('--platform') + 1] == 'linux/amd64', call
    for name, eco, extra in [
        ('channel-injection', 'pypi', ('--pytorch-channel', '../cpu')),
        ('channel-on-npm', 'npm', ('--pytorch-channel', 'cpu')),
        ('wrong-platform', 'pypi', ('--pytorch-channel', 'cpu', '--platform', 'linux/arm64')),
    ]:
        result, calls, report = run_case(name, eco=eco, extra=extra)
        assert result.returncode != 0 and not calls and report is None, (result, calls, report)

    result, calls, report = run_case('digest-identity', extra=('--pytorch-channel', 'cpu'), failure='repo-digest')
    assert result.returncode == 0 and report['environment']['client_image'] == 'fixture@sha256:immutable', (result, report)
    assert all(call['image'] == 'fixture@sha256:immutable' for call in calls), calls
    result, calls, report = run_case('python-profile-mismatch', extra=('--pytorch-channel', 'cpu'), failure='python-version')
    assert result.returncode != 0 and not calls and report is None, (result, calls, report)
    vllm_input = Path(script).resolve().parent.parent / 'testground' / 'benchmark' / 'vllm-pypi'
    result, calls, report = run_case('vllm-lock', extra=('--input-dir', str(vllm_input), '--platform', 'linux/amd64'))
    assert result.returncode == 0 and 'profile.json' in report['inputs'], (result, report)
    assert report['environment']['direct_endpoint'] == 'https://pypi.org/simple/', report

    result, calls, report = run_case('explicit-timeout', extra=('--pip-timeout', '90'))
    assert result.returncode == 0 and report['environment']['pip_socket_timeout_seconds'] == 90, (result, report)
    assert all(call['command'][call['command'].index('--timeout') + 1] == '90' for call in calls if call['kind'] == 'install'), calls
    result, calls, report = run_case('invalid-timeout', extra=('--pip-timeout', '0'))
    assert result.returncode == 2 and not calls and report is None, (result, calls, report)

    fixture = root / 'custom-npm'
    fixture.mkdir()
    source = Path(script).resolve().parent.parent / 'testground' / 'benchmark' / 'npm'
    for name in ('package.json', 'package-lock.json'):
        (fixture / name).write_bytes((source / name).read_bytes())
    result, calls, report = run_case('locked-custom', eco='npm', extra=('--input-dir', str(fixture)))
    assert result.returncode == 0 and report['package'] == 'custom locked input', (result, report)
    lock = json.loads((fixture / 'package-lock.json').read_text())
    lock['packages']['node_modules/lodash']['resolved'] = 'https://other-registry.example/lodash.tgz'
    (fixture / 'package-lock.json').write_text(json.dumps(lock))
    result, calls, report = run_case('bypass-registry', eco='npm', extra=('--input-dir', str(fixture)))
    assert result.returncode != 0 and not calls and report is None, (result, calls, report)

    requirements = root / 'custom-pypi'
    requirements.mkdir()
    (requirements / 'requirements.txt').write_text('requests @ https://other-registry.example/requests.whl --hash=sha256:' + '0' * 64 + '\n')
    result, calls, report = run_case('bypass-pypi', extra=('--input-dir', str(requirements)))
    assert result.returncode != 0 and not calls and report is None, (result, calls, report)

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

    for failure in ('docker-version', 'image-pull', 'client-version'):
        result, calls, report = run_case(failure, failure=failure)
        assert result.returncode == 1 and not calls and report is None, (failure, result, calls, report)

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
