"""Offline checks for acceptance guards; these do not qualify a Depsilo release."""

import copy
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

import client
import run


class IsolationTest(unittest.TestCase):
    def setUp(self):
        self.target = {
            "Id": "target-id", "Image": "target-image", "State": {"Running": True},
            "HostConfig": {"Privileged": False, "NetworkMode": "offline"},
            "NetworkSettings": {"Networks": {"offline": {
                "NetworkID": "network-id", "IPAddress": "172.25.0.2",
                "Aliases": ["depsilo"], "DNSNames": ["depsilo"],
            }}},
        }
        self.network = {"Id": "network-id", "Driver": "bridge", "Internal": True,
                        "Containers": {"target-id": {}}}

    def test_accepts_only_target_alias_or_ip(self):
        for url in ("http://depsilo:23333/pypi/simple/", "http://172.25.0.2:23333/offline/v1/simple/"):
            run.validate_target(self.target, self.network, url)
        for url in ("https://pypi.org/simple/", "http://localhost:23333/pypi/simple/",
                    "http://user:password@depsilo:23333/pypi/simple/",
                    "http://depsilo:23333/pypi/simple/?token=secret",
                    "http://depsilo:23333/pypi/simple/#fragment", "file:///simple/",
                    "http://depsilo:bad/pypi/simple/", "http://depsilo:23333/pypi/simple"):
            with self.subTest(url=url), self.assertRaises(ValueError):
                run.validate_target(self.target, self.network, url)

    def test_rejects_external_or_shared_network(self):
        for key, value in (("Internal", False), ("Driver", "host"),
                           ("Containers", {"target-id": {}, "proxy-id": {}})):
            network = dict(self.network, **{key: value})
            with self.subTest(key=key), self.assertRaises(ValueError):
                run.validate_target(self.target, network, "http://depsilo/pypi/simple/")
        self.target["NetworkSettings"]["Networks"]["outside"] = {"NetworkID": "external"}
        with self.assertRaises(ValueError):
            run.validate_target(self.target, self.network, "http://depsilo/pypi/simple/")

    def test_rejects_stopped_or_privileged_target(self):
        for change in ({"Privileged": True}, {"NetworkMode": "host"},
                       {"NetworkMode": "container:other"}, {"CapAdd": ["NET_ADMIN"]},
                       {"CapAdd": ["CAP_SYS_ADMIN"]}, {"CapAdd": ["ALL"]}):
            target = copy.deepcopy(self.target)
            target["HostConfig"].update(change)
            with self.subTest(change=change), self.assertRaises(ValueError):
                run.validate_target(target, self.network, "http://depsilo/pypi/simple/")
        self.target["State"]["Running"] = False
        with self.assertRaises(ValueError):
            run.validate_target(self.target, self.network, "http://depsilo/pypi/simple/")

    def test_probe_timeout_records_failure_and_removes_its_container(self):
        with tempfile.TemporaryDirectory() as temporary:
            output = Path(temporary) / "evidence"
            argv = ["run.py", "--target", "target-id", "--network", "offline",
                    "--index-url", "http://depsilo/pypi/simple/", "--out", str(output)]
            image = {"Id": "image-id", "Os": "linux", "Architecture": "amd64"}
            with patch.object(sys, "argv", argv), \
                    patch.object(run, "inspect", side_effect=[image, self.target, self.network]), \
                    patch.object(run, "docker", side_effect=subprocess.TimeoutExpired("docker", 30)) as start, \
                    patch.object(run.subprocess, "run") as cleanup:
                with self.assertRaises(subprocess.TimeoutExpired):
                    run.main()
            probe_name = start.call_args.args[start.call_args.args.index("--name") + 1]
            self.assertIn("@sha256:", start.call_args.args[-4])
            self.assertEqual(cleanup.call_args.args[0], ["docker", "rm", "--force", probe_name])
            self.assertEqual(json.loads((output / "run.json").read_text())["status"], "failed")


class InstallEvidenceTest(unittest.TestCase):
    def setUp(self):
        self.profile = {"packages": {"requests": "2.32.3"}}
        self.report = {"install": [{
            "metadata": {"name": "requests", "version": "2.32.3"},
            "download_info": {"url": "http://depsilo:23333/files/requests-2.32.3-py3-none-any.whl",
                              "archive_info": {"hashes": {"sha256": "a" * 64}}},
        }]}

    def test_requires_exact_closure_and_target_artifacts(self):
        self.assertEqual(client.check_report(self.report, self.profile, "http://depsilo:23333/pypi/simple/"),
                         {"requests": "2.32.3"})
        for url in ("https://files.pythonhosted.org/requests.whl", "http://depsilo:23333/requests.tar.gz"):
            report = copy.deepcopy(self.report)
            report["install"][0]["download_info"]["url"] = url
            with self.subTest(url=url), self.assertRaises(RuntimeError):
                client.check_report(report, self.profile, "http://depsilo:23333/pypi/simple/")
        for items in ([], self.report["install"] * 2):
            with self.subTest(items=items), self.assertRaises(RuntimeError):
                client.check_report({"install": items}, self.profile, "http://depsilo:23333/pypi/simple/")
        self.report["install"][0]["metadata"]["version"] = "2.31.0"
        with self.assertRaises(RuntimeError):
            client.check_report(self.report, self.profile, "http://depsilo:23333/pypi/simple/")

    def test_missing_digest_is_not_install_evidence(self):
        self.report["install"][0]["download_info"]["archive_info"] = {}
        with self.assertRaises(RuntimeError):
            client.check_report(self.report, self.profile, "http://depsilo:23333/pypi/simple/")


if __name__ == "__main__":
    unittest.main()
