#!/usr/bin/env python3
"""Native-client evidence from disposable Depsilo nodes; never touches an existing instance."""

import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import secrets
import statistics
import subprocess
import tempfile
import time


ROOT = Path(__file__).resolve().parent.parent
CLIENT_IMAGES = {
    "pypi": "python:3.11-slim@sha256:db3ff2e1800a8581e2c48a27c3995339d47bdf046da21c7627accd3d51053a93",
    "npm": "node:20-alpine@sha256:fb4cd12c85ee03686f6af5362a0b0d56d50c58a04632e6c0fb8363f609372293",
}
ORIGINS = {"pypi": "https://pypi.org", "npm": "https://registry.npmjs.org"}
SCENARIOS = ("direct_client_cold", "single_cold", "single_hot_client_cold",
             "cascade_first_child_cold", "cascade_second_child_cold")
COUNTERS = {
    "direct_bytes": ('depsilo_upstream_response_bytes_total', 'direct'),
    "peer_bytes": ('depsilo_upstream_response_bytes_total', 'peer'),
    "direct_exchanges": ('depsilo_upstream_exchanges_total', 'direct'),
    "peer_exchanges": ('depsilo_upstream_exchanges_total', 'peer'),
    "direct_redirect_bytes": ('depsilo_upstream_redirect_response_bytes_total', 'direct'),
    "peer_redirect_bytes": ('depsilo_upstream_redirect_response_bytes_total', 'peer'),
    "direct_redirect_exchanges": ('depsilo_upstream_redirect_exchanges_total', 'direct'),
    "peer_redirect_exchanges": ('depsilo_upstream_redirect_exchanges_total', 'peer'),
    "service_bytes": ('depsilo_service_response_bytes_total', None),
}


def parse_counters(body):
    """Missing counters are an incompatible binary, never a zero-byte cache hit."""
    result = {}
    for key, (metric, route) in COUNTERS.items():
        labels = r'\{route="' + route + r'"\}' if route else ''
        matches = re.findall(r'^' + re.escape(metric) + labels + r' ([0-9.eE+\-]+)$', body, re.MULTILINE)
        if len(matches) != 1:
            raise ValueError(f"missing or ambiguous metric: {metric} route={route}")
        value = float(matches[0])
        if not value.is_integer() or value < 0:
            raise ValueError(f"invalid counter: {key}")
        result[key] = int(value)
    return result


def counter_delta(before, after):
    result = {key: after[key] - before[key] for key in COUNTERS}
    if any(value < 0 for value in result.values()):
        raise ValueError("counter reset during sample; discard this run")
    for route in ("direct", "peer"):
        for quantity in ("bytes", "exchanges"):
            if result[f"{route}_redirect_{quantity}"] > result[f"{route}_{quantity}"]:
                raise ValueError("redirect subset exceeds upstream total; discard this run")
    return result


def cache_evidence(scenario, deltas):
    """Prove the claimed topology using both traffic and fresh-node identities."""
    if scenario == "direct_client_cold":
        return None
    if scenario == "single_cold":
        return (deltas["single"]["direct_bytes"] > deltas["single"]["direct_redirect_bytes"] and deltas["single"]["peer_exchanges"] == 0
                and deltas["single"]["service_bytes"] > 0)
    if scenario == "single_hot_client_cold":
        return (no_content_fetch(deltas["single"]) and deltas["single"]["peer_exchanges"] == 0
                and deltas["single"]["service_bytes"] > 0)
    child = "child-a" if scenario == "cascade_first_child_cold" else "child-b"
    used_parent = (deltas[child]["peer_bytes"] > 0 and deltas[child]["direct_exchanges"] == 0
                   and deltas[child]["service_bytes"] > 0)
    if scenario == "cascade_first_child_cold":
        return used_parent and deltas["parent"]["direct_bytes"] > deltas["parent"]["direct_redirect_bytes"]
    return used_parent and no_content_fetch(deltas["parent"])


def no_content_fetch(counters):
    # A cold child may need an uncached PyPI Location lookup before fetching
    # an already cached wheel. Retain that traffic; it is not artifact reuse.
    return (counters["direct_exchanges"] == counters["direct_redirect_exchanges"]
            and counters["direct_bytes"] == counters["direct_redirect_bytes"])


class Docker:
    def __init__(self, evidence):
        self.evidence = evidence

    def run(self, args, *, log=None, check=True, timeout=120):
        try:
            result = subprocess.run(["docker", *args], capture_output=True, text=True, timeout=timeout)
        except subprocess.TimeoutExpired as error:
            if log:
                def decoded(value):
                    return value.decode(errors="replace") if isinstance(value, bytes) else (value or "")
                (self.evidence / log).write_text(decoded(error.stdout) + decoded(error.stderr) + "\nDocker command timed out.\n")
            raise
        if log:
            (self.evidence / log).write_text(result.stdout + result.stderr)
        if check and result.returncode:
            raise RuntimeError(f"docker {args[0]} failed ({result.returncode}); {log or result.stderr[-800:]}")
        return result

    def image(self, reference, platform, name):
        # Image pulls happen before timing or topology creation.
        result = self.run(["image", "inspect", "--platform", platform, reference], check=False)
        records = json.loads(result.stdout) if result.returncode == 0 else []
        if not records or records[0]["Os"] + "/" + records[0]["Architecture"] != platform:
            self.run(["pull", "--platform", platform, reference], log=name + "-pull.log", timeout=600)
            records = json.loads(self.run(["image", "inspect", "--platform", platform, reference]).stdout)
        record = records[0]
        if record["Os"] + "/" + record["Architecture"] != platform:
            raise ValueError(f"{name} image does not match {platform}")
        (self.evidence / (name + "-image.json")).write_text(json.dumps(records, indent=2) + "\n")
        # Containerd may return a platform manifest ID that `docker run` cannot
        # resolve by bare sha256. Prefer the repository digest with --platform.
        digests = record.get("RepoDigests", [])
        return digests[0] if digests else record["Id"]


class Topology:
    def __init__(self, docker, image, platform, eco, config_dir, run):
        self.docker, self.image, self.platform, self.eco = docker, image, platform, eco
        self.config_dir, self.run_number = config_dir, run
        self.network = "depsilo-benchmark-" + secrets.token_hex(6)
        self.egress_network = self.network + "-egress"
        self.nodes = {}
        self.containers = []
        self.clients = set()
        self.networks = []
        self.cleanup_errors = []
        self.token = secrets.token_hex(32)

    def __enter__(self):
        try:
            for network, flags in ((self.network, ["--internal"]), (self.egress_network, [])):
                self.networks.append(network)
                self.docker.run(["network", "create", *flags, "--label", "depsilo.benchmark=true", network])
        except BaseException:
            self.__exit__(True, None, None)
            raise
        return self

    def start(self, name, parent=None):
        path = self.config_dir / f"{name}.toml"
        config = f'''[server]
host = "0.0.0.0"
port = 23333
[database]
driver = "sqlite"
dsn = "/tmp/depsilo-benchmark/state.db"
[storage]
type = "local"
path = "/tmp/depsilo-benchmark/cache"
[cache]
max_size_gb = 1
ttl_index = "1h"
ttl_blob = "24h"
[auth]
enabled = false
jwt_secret = "{secrets.token_hex(32)}"
[supply_chain]
min_release_age_enabled = false
[supply_chain.blocklist]
enabled = false
[upstream_updates]
enabled = false
[cascade]
enabled = true
token = "{self.token}"
allow_insecure_http = true
'''
        if parent:
            config += f'''[[cascade.peers]]
name = "parent"
url = "http://{parent}:23333"
'''
        config += f'''[[{self.eco}.upstreams]]
name = "benchmark"
url = "{ORIGINS[self.eco]}"
priority = 1
probe_mode = "passive"
probe_interval = "24h"
'''
        if parent:
            config += 'via = "parent"\n'
        path.write_text(config)
        # The production image runs unprivileged; the enclosing temporary
        # directory is private, and only this one file is mounted read-only.
        path.chmod(0o644)
        container = self.network + "-" + name
        self.containers.append(container)
        self.docker.run(["run", "-d", "--name", container, "--platform", self.platform,
                         "--network", self.network, "--network-alias", name,
                         "--label", "depsilo.benchmark=true", "--mount",
                         f"type=bind,src={path},dst=/tmp/benchmark.toml,readonly",
                         self.image, "serve", "--config", "/tmp/benchmark.toml"])
        self.nodes[name] = container
        if not parent:
            self.docker.run(["network", "connect", self.egress_network, container])
        deadline = time.monotonic() + 60
        while time.monotonic() < deadline:
            result = self.docker.run(["exec", container, "wget", "-q", "-O", "-",
                                      "http://127.0.0.1:23333/ready"], check=False, timeout=5)
            if result.returncode == 0:
                if json.loads(result.stdout).get("status") == "ready":
                    return
            time.sleep(0.2)
        raise RuntimeError(f"{name} did not become ready; inspect retained server log")

    def snapshot(self, label):
        readings = {}
        for name, container in self.nodes.items():
            result = self.docker.run(["exec", container, "wget", "-q", "-O", "-",
                                      "http://127.0.0.1:23333/metrics"],
                                     log=f"run-{self.run_number}-{label}-{name}.prom")
            readings[name] = parse_counters(result.stdout)
        return readings

    def __exit__(self, exc_type, *_):
        def cleanup(args, **kwargs):
            try:
                result = self.docker.run(args, check=False, timeout=15, **kwargs)
                if result.returncode:
                    self.cleanup_errors.append(" ".join(args) + f" exited {result.returncode}")
            except (OSError, subprocess.SubprocessError) as error:
                self.cleanup_errors.append(" ".join(args) + ": " + str(error))

        for container in sorted(self.clients):
            cleanup(["logs", container], log=container + ".log")
            cleanup(["rm", "-f", container])
        for container in reversed(self.containers):
            cleanup(["logs", container], log=container + ".log")
            cleanup(["rm", "-f", container])
        for network in reversed(self.networks):
            cleanup(["network", "rm", network])
        if self.cleanup_errors:
            (self.docker.evidence / f"run-{self.run_number}-cleanup-errors.json").write_text(json.dumps(self.cleanup_errors, indent=2) + "\n")
            if exc_type is None:
                raise RuntimeError("resource cleanup incomplete; inspect retained cleanup-errors.json")


def install(docker, topology, image, inputs, eco, endpoint, label):
    container = topology.network + "-client"
    network = topology.egress_network if endpoint.startswith("https://") else topology.network
    args = ["run", "--name", container, "--platform", topology.platform,
            "--network", network, "--mount", f"type=bind,src={inputs},dst=/input,readonly",
            "--workdir", "/tmp/install", image]
    if eco == "pypi":
        args += ["python", "-m", "pip", "--isolated", "install", "--disable-pip-version-check",
                 "--no-input", "--no-cache-dir", "--require-hashes", "--only-binary=:all:",
                 "--ignore-installed", "--target", "/tmp/install", "--index-url", endpoint,
                 "-r", "/input/requirements.txt"]
        if endpoint.startswith("http://"):
            args += ["--trusted-host", endpoint.split("/")[2]]
    else:
        # Only constant script text is executed; the endpoint is a positional
        # argument, so no caller input is interpolated into shell code.
        args += ["sh", "-ec", 'cp /input/package*.json .; npm ci --ignore-scripts --no-audit '
                 '--no-fund --registry "$1"; node -e "require(\'lodash\')"', "benchmark", endpoint]
    start = time.monotonic_ns()
    topology.clients.add(container)
    try:
        result = docker.run(args, log=label + ".log", check=False, timeout=600)
        elapsed = (time.monotonic_ns() - start) / 1e9
    finally:
        # Also remove a client left running after a host timeout/interruption.
        removed = docker.run(["rm", "-f", container], check=False)
        if removed.returncode == 0:
            topology.clients.discard(container)
    return result.returncode, elapsed


def run(args):
    out = args.out.resolve()
    out.mkdir(parents=True, exist_ok=False)
    out.chmod(0o700)
    docker = Docker(out)
    report = {"status": "running", "ecosystem": args.ecosystem, "runs": args.runs,
              "samples": [], "summary": [], "controlled_cache_evidence": False,
              "measurement_scope": "Successful upstream HTTP response-body bytes, per node and route; not billed WAN traffic.",
              "limitations": ["Timing includes container startup and install; image pulls and server startup are excluded.",
                              "Response-body reads can be after HTTP transport decompression; these are not encoded wire-byte measurements.",
                              "Public registry and network conditions vary; no speedup guarantee.",
                              "Client caches and install directories are empty for every sample.",
                              "Direct-client upstream bytes are not instrumented and are reported as null.",
                              "Warm reuse permits observed redirect lookups, recorded as a subset of total upstream traffic; 304 revalidation is not a redirect.",
                              "These fixtures disable release-age and malware blocklist checks; provenance and tamper checks remain enabled.",
                              "Docker OCI, compiler caches and offline delivery are outside this benchmark."]}
    try:
        docker.run(["version"], log="docker-version.log")
        server = docker.image(args.server_image, args.platform, "server")
        client = docker.image(CLIENT_IMAGES[args.ecosystem], args.platform, "client")
        server_version = docker.run(["run", "--rm", "--platform", args.platform, "--network", "none", server, "version"], log="server-version.log").stdout.strip()
        version_cmd = ["python", "-m", "pip", "--version"] if args.ecosystem == "pypi" else ["npm", "--version"]
        docker.run(["run", "--rm", "--platform", args.platform, "--network", "none", client, *version_cmd], log="client-version.log")
        inputs = out / "input"
        inputs.mkdir()
        names = ["requirements.txt"] if args.ecosystem == "pypi" else ["package.json", "package-lock.json"]
        for name in names:
            (inputs / name).write_bytes((ROOT / "testground/benchmark" / args.ecosystem / name).read_bytes())
        report.update(server_image=server, client_image=client, server_version=server_version,
                      platform=args.platform, inputs={p.name: hashlib.sha256(p.read_bytes()).hexdigest() for p in inputs.iterdir()})
        for number in range(1, args.runs + 1):
            with tempfile.TemporaryDirectory(prefix="depsilo-benchmark-config-") as directory:
                with Topology(docker, server, args.platform, args.ecosystem, Path(directory), number) as topology:
                    topology.start("single")
                    topology.start("parent")
                    topology.start("child-a", "parent")
                    topology.start("child-b", "parent")
                    report.setdefault("topologies", []).append({"run": number, "internal_network": topology.network,
                                                                "egress_network": topology.egress_network,
                                                                "nodes": dict(topology.nodes), "storage": "fresh container filesystem; no host state mounts",
                                                                "cleanup_errors": topology.cleanup_errors})
                    for scenario in SCENARIOS:
                        node = {"direct_client_cold": None, "single_cold": "single", "single_hot_client_cold": "single",
                                "cascade_first_child_cold": "child-a", "cascade_second_child_cold": "child-b"}[scenario]
                        endpoint = (f"http://{node}:23333/{args.ecosystem}" if node else ORIGINS[args.ecosystem])
                        endpoint += "/simple/" if args.ecosystem == "pypi" else "/"
                        label = f"run-{number}-{scenario}"
                        before = topology.snapshot(scenario + "-before")
                        code, elapsed = install(docker, topology, client, inputs, args.ecosystem, endpoint, label)
                        after = topology.snapshot(scenario + "-after")
                        deltas = {name: counter_delta(before[name], after[name]) for name in before}
                        proof = cache_evidence(scenario, deltas)
                        row = {"run": number, "scenario": scenario, "status": "passed" if code == 0 and proof is not False else "failed",
                               "install_exit_code": code, "wall_seconds": elapsed, "client_cache_state": "cold",
                               "cache_expectation_verified": proof, "node_traffic": deltas if node else None, "log": label + ".log"}
                        report["samples"].append(row)
                        print(f"run {number}: {scenario}: {row['status']} ({elapsed:.2f}s)", flush=True)
                        if row["status"] != "passed":
                            raise RuntimeError(f"{scenario} failed installation or cache evidence; inspect {label}.log and metric snapshots")
        for scenario in SCENARIOS:
            values = [r["wall_seconds"] for r in report["samples"] if r["scenario"] == scenario]
            report["summary"].append({"scenario": scenario, "samples": len(values), "median_wall_seconds": statistics.median(values)})
        report["controlled_cache_evidence"] = True
        report["status"] = "complete"
    except (OSError, ValueError, RuntimeError, subprocess.SubprocessError, KeyboardInterrupt) as error:
        report["status"] = "failed"
        report["error"] = str(error) or "interrupted"
    finally:
        (out / "report.json").write_text(json.dumps(report, indent=2) + "\n")
        print(f"Evidence: {out / 'report.json'}", flush=True)
    return 0 if report["status"] == "complete" else 1


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--server-image", required=True, help="Candidate Depsilo runtime image with wget and the new traffic metrics")
    parser.add_argument("--ecosystem", choices=CLIENT_IMAGES, required=True)
    parser.add_argument("--platform", choices=("linux/amd64", "linux/arm64"), default="linux/amd64")
    parser.add_argument("--runs", type=int, default=5)
    parser.add_argument("--out", type=Path, required=True, help="New private evidence directory; existing paths are never overwritten")
    args = parser.parse_args()
    if not 1 <= args.runs <= 20:
        parser.error("--runs must be 1..20")
    return run(args)


if __name__ == "__main__":
    raise SystemExit(main())
