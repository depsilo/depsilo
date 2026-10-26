#!/usr/bin/env python3
"""Offline contracts for the controlled benchmark; no network or real Docker calls."""

import importlib.util
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch


sys.dont_write_bytecode = True
spec = importlib.util.spec_from_file_location("benchmark_cascade", Path(__file__).with_name("benchmark-cascade.py"))
benchmark = importlib.util.module_from_spec(spec)
spec.loader.exec_module(benchmark)


def metrics(direct_bytes=0, peer_bytes=0, direct_exchanges=0, peer_exchanges=0, service_bytes=0,
            direct_redirect_bytes=0, peer_redirect_bytes=0, direct_redirect_exchanges=0, peer_redirect_exchanges=0):
    values = locals()
    return "\n".join(metric + (f'{{route="{route}"}}' if route else '') + f' {values[key]}'
                     for key, (metric, route) in benchmark.COUNTERS.items())


class BenchmarkContracts(unittest.TestCase):
    def test_missing_or_reset_metrics_cannot_prove_cache_reuse(self):
        before = benchmark.parse_counters(metrics(100, 20, 2, 1))
        after = benchmark.parse_counters(metrics(100, 70, 2, 2))
        expected = benchmark.parse_counters(metrics(peer_bytes=50, peer_exchanges=1))
        self.assertEqual(benchmark.counter_delta(before, after), expected)
        for broken in ("", metrics() + "\n" + metrics(), metrics().replace(" 0", " -1", 1)):
            with self.assertRaises(ValueError):
                benchmark.parse_counters(broken)
        with self.assertRaises(ValueError):
            benchmark.counter_delta(after, before)

    def test_second_child_must_use_parent_without_new_direct_exchange(self):
        zero = benchmark.parse_counters(metrics())
        delta = {"parent": dict(zero), "child-b": benchmark.parse_counters(metrics(peer_bytes=42, peer_exchanges=1, service_bytes=42))}
        self.assertTrue(benchmark.cache_evidence("cascade_second_child_cold", delta))
        for node, key, value in (("child-b", "peer_bytes", 0), ("child-b", "direct_exchanges", 1), ("child-b", "service_bytes", 0),
                                 ("parent", "direct_exchanges", 1)):
            changed = {name: dict(values) for name, values in delta.items()}
            changed[node][key] = value
            self.assertFalse(benchmark.cache_evidence("cascade_second_child_cold", changed))
        self.assertIsNone(benchmark.cache_evidence("direct_client_cold", {}))

    def test_remaining_redirect_cost_is_preserved_without_claiming_content_download(self):
        zero = benchmark.parse_counters(metrics())
        redirected = benchmark.parse_counters(metrics(direct_bytes=200, direct_exchanges=2,
                                                       direct_redirect_bytes=200, direct_redirect_exchanges=2))
        delta = {"parent": benchmark.counter_delta(zero, redirected),
                 "child-b": benchmark.parse_counters(metrics(peer_bytes=5000, peer_exchanges=2, service_bytes=5000))}
        self.assertTrue(benchmark.cache_evidence("cascade_second_child_cold", delta))
        self.assertEqual(delta["parent"]["direct_bytes"], 200)
        delta["parent"]["direct_exchanges"] += 1
        self.assertFalse(benchmark.cache_evidence("cascade_second_child_cold", delta))
        with self.assertRaisesRegex(ValueError, "subset"):
            benchmark.counter_delta(zero, benchmark.parse_counters(metrics(direct_redirect_exchanges=1)))

    def test_cold_first_fill_and_hot_reuse_are_separate_assertions(self):
        delta = {"single": benchmark.parse_counters(metrics(42, 0, 1, 0, 42)),
                 "parent": benchmark.parse_counters(metrics(42, 0, 1, 0)),
                 "child-a": benchmark.parse_counters(metrics(0, 42, 0, 1, 42))}
        self.assertTrue(benchmark.cache_evidence("single_cold", delta))
        self.assertFalse(benchmark.cache_evidence("single_hot_client_cold", delta))
        self.assertTrue(benchmark.cache_evidence("cascade_first_child_cold", delta))
        delta["single"] = benchmark.parse_counters(metrics(service_bytes=42))
        self.assertFalse(benchmark.cache_evidence("single_cold", delta))
        self.assertTrue(benchmark.cache_evidence("single_hot_client_cold", delta))

    def test_failed_install_retains_report_and_releases_owned_resources(self):
        calls = []

        class FakeDocker:
            def __init__(self, evidence):
                self.evidence = evidence

            def image(self, reference, platform, name):
                return "sha256:fixture-" + name

            def run(self, args, **kwargs):
                calls.append(args)
                output = ""
                code = 0
                if args[0] == "exec":
                    output = '{"status":"ready"}' if args[-1].endswith("/ready") else metrics()
                if "--workdir" in args:
                    output, code = "fixture installation failed", 17
                if kwargs.get("log"):
                    (self.evidence / kwargs["log"]).write_text(output)
                return subprocess.CompletedProcess(args, code, output, "")

        with tempfile.TemporaryDirectory() as directory:
            out = Path(directory) / "evidence"
            args = type("Args", (), dict(out=out, ecosystem="npm", runs=1, server_image="fixture", platform="linux/amd64"))()
            with patch.object(benchmark, "Docker", FakeDocker):
                self.assertEqual(benchmark.run(args), 1)
            report = json.loads((out / "report.json").read_text())
            self.assertEqual(report["status"], "failed")
            self.assertFalse(report["controlled_cache_evidence"])
            self.assertEqual(report["samples"][0]["install_exit_code"], 17)
            self.assertIsNone(report["samples"][0]["node_traffic"])
        starts = [call for call in calls if call[:2] == ["run", "-d"]]
        self.assertEqual(len(starts), 4)
        names = [call[call.index("--name") + 1] for call in starts]
        for name in names:
            self.assertIn(["logs", name], calls)
            self.assertIn(["rm", "-f", name], calls)
        networks = [call[-1] for call in calls if call[:2] == ["network", "create"]]
        self.assertEqual(len(networks), 2)
        for network in networks:
            self.assertIn(["network", "rm", network], calls)
        self.assertIn("--internal", next(call for call in calls if call[:2] == ["network", "create"]))
        connected = [call[-1] for call in calls if call[:2] == ["network", "connect"]]
        self.assertCountEqual(connected, [names[0], names[1]])
        self.assertTrue(all("-p" not in call and "--publish" not in call for call in starts))

    def test_cleanup_continues_after_a_timeout(self):
        with tempfile.TemporaryDirectory() as directory:
            docker = benchmark.Docker(Path(directory))
            calls = []

            def command(args, **_):
                calls.append(args)
                if args[0] == "logs":
                    raise subprocess.TimeoutExpired(args, 15)
                return subprocess.CompletedProcess(args, 0, "", "")

            docker.run = command
            topology = benchmark.Topology(docker, "image", "linux/amd64", "npm", Path(directory), 1)
            topology.containers = ["owned-a", "owned-b"]
            topology.networks = ["owned-net"]
            with self.assertRaisesRegex(RuntimeError, "cleanup incomplete"):
                topology.__exit__(None, None, None)
            self.assertIn(["rm", "-f", "owned-a"], calls)
            self.assertIn(["rm", "-f", "owned-b"], calls)
            self.assertIn(["network", "rm", "owned-net"], calls)
            self.assertEqual(len(json.loads((Path(directory) / "run-1-cleanup-errors.json").read_text())), 2)

    def test_native_clients_use_internal_network_except_direct_baseline(self):
        with tempfile.TemporaryDirectory() as directory:
            evidence = Path(directory)
            inputs = evidence / "input"
            inputs.mkdir()
            docker = benchmark.Docker(evidence)
            calls = []

            def command(args, **_):
                calls.append(args)
                return subprocess.CompletedProcess(args, 0, "", "")

            docker.run = command
            for ecosystem in benchmark.CLIENT_IMAGES:
                topology = benchmark.Topology(docker, "server", "linux/amd64", ecosystem, evidence, 1)
                for endpoint, network in ((benchmark.ORIGINS[ecosystem], topology.egress_network),
                                          ("http://child-b:23333/" + ecosystem, topology.network)):
                    with self.subTest(ecosystem=ecosystem, endpoint=endpoint):
                        calls.clear()
                        code, _ = benchmark.install(docker, topology, "client", inputs, ecosystem, endpoint, "sample")
                        self.assertEqual(code, 0)
                        run = calls[0]
                        self.assertEqual(run[run.index("--network") + 1], network)
                        self.assertEqual(run[run.index("--platform") + 1], "linux/amd64")
                        self.assertEqual(run[run.index("--workdir") + 1], "/tmp/install")
                        # Only inputs are mounted. Package caches and install
                        # directories remain inside each disposable client.
                        self.assertEqual(run.count("--mount"), 1)
                        self.assertEqual(run[run.index("--mount") + 1],
                                         f"type=bind,src={inputs},dst=/input,readonly")
                        self.assertNotIn("--volumes-from", run)
                        self.assertIn(endpoint, run)
                        name = run[run.index("--name") + 1]
                        self.assertIn(["rm", "-f", name], calls)
                        self.assertEqual(topology.clients, set())

    def test_client_timeout_keeps_failed_removal_owned_for_cleanup_retry(self):
        with tempfile.TemporaryDirectory() as directory:
            docker = benchmark.Docker(Path(directory))
            topology = benchmark.Topology(docker, "server", "linux/amd64", "npm", Path(directory), 1)
            calls = []
            removals = 0

            def command(args, **_):
                nonlocal removals
                calls.append(args)
                if args[0] == "run":
                    raise subprocess.TimeoutExpired(args, 600)
                if args[:2] == ["rm", "-f"]:
                    removals += 1
                    return subprocess.CompletedProcess(args, 1 if removals == 1 else 0, "", "")
                return subprocess.CompletedProcess(args, 0, "", "")

            docker.run = command
            with self.assertRaises(subprocess.TimeoutExpired):
                benchmark.install(docker, topology, "client", Path(directory), "npm",
                                  "http://child-b:23333/npm/", "sample")
            client = topology.network + "-client"
            self.assertEqual(topology.clients, {client})
            topology.__exit__(subprocess.TimeoutExpired, None, None)
            self.assertEqual(removals, 2)
            self.assertIn(["logs", client], calls)


if __name__ == "__main__":
    unittest.main()
