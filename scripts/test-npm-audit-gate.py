#!/usr/bin/env python3
"""Offline contracts for scripts/npm-audit-gate.py; npm is never run."""

import contextlib
import datetime
import importlib.util
import io
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch


sys.dont_write_bytecode = True
spec = importlib.util.spec_from_file_location("npm_audit_gate", Path(__file__).with_name("npm-audit-gate.py"))
gate = importlib.util.module_from_spec(spec)
spec.loader.exec_module(gate)

PICOMATCH = {
    "source": 1247710,
    "name": "picomatch",
    "dependency": "picomatch",
    "title": "picomatch vulnerable to ReDoS through single-branch repeated extglobs",
    "url": "https://github.com/advisories/GHSA-fc5q-39xc-vjpf",
    "severity": "high",
    "range": "<=4.0.7",
}
RANK = {"info": 0, "low": 1, "moderate": 2, "high": 3, "critical": 4}


def report(*advisories, dependents=("fdir", "tinyglobby", "vite")):
    """An npm audit --json report (version 2) in the shape npm 10 prints: one
    entry per package with advisories, listing them all in via, and entries
    for the packages that depend on it, which name it as a string. A
    package's severity and the metadata counts are the highest severity of
    its advisories, as npm computes them."""
    vulnerabilities = {}
    for advisory in advisories:
        package = advisory["dependency"]
        entry = vulnerabilities.setdefault(package, {
            "name": package, "severity": "info", "isDirect": False, "via": [],
            "effects": list(dependents), "range": advisory.get("range", "*"), "fixAvailable": False,
        })
        entry["via"].append(advisory)
        for dependent in dependents:
            dependent_entry = vulnerabilities.setdefault(dependent, {
                "name": dependent, "severity": "info", "isDirect": False, "via": [],
                "effects": [], "range": "*", "fixAvailable": False,
            })
            if package not in dependent_entry["via"]:
                dependent_entry["via"].append(package)
    for entry in vulnerabilities.values():
        severities = [via.get("severity", "high") for via in entry["via"] if isinstance(via, dict)]
        severities += [vulnerabilities[name]["severity"] for name in entry["via"] if isinstance(name, str)]
        entry["severity"] = max(severities or ["info"], key=lambda severity: RANK.get(severity, 5))
    counts = {severity: 0 for severity in RANK}
    for entry in vulnerabilities.values():
        if entry["severity"] in counts:
            counts[entry["severity"]] += 1
    counts["total"] = len(vulnerabilities)
    return {"auditReportVersion": 2, "vulnerabilities": vulnerabilities, "metadata": {"vulnerabilities": counts}}


def entry(advisory_id="GHSA-fc5q-39xc-vjpf", package="picomatch", expires="2027-01-31", reason="no patched release"):
    return {"id": advisory_id, "package": package, "reason": reason, "expires": expires}


class NpmAuditGate(unittest.TestCase):
    def run_gate(self, audit, entries, today="2026-11-18", extra=()):
        """Runs the gate on a saved report, or with npm stubbed out when audit
        is None, and returns its status, stdout and stderr."""
        with tempfile.TemporaryDirectory() as directory:
            allowlist_path = Path(directory) / "allowlist.json"
            allowlist_path.write_text(json.dumps({"advisories": entries}))
            arguments = ["--allowlist", str(allowlist_path), "--today", today, *extra]
            if audit is not None:
                report_path = Path(directory) / "audit.json"
                report_path.write_text(json.dumps(audit))
                arguments += ["--report", str(report_path)]
            stdout, stderr = io.StringIO(), io.StringIO()
            with contextlib.redirect_stdout(stdout), contextlib.redirect_stderr(stderr):
                status = gate.main(arguments)
            return status, stdout.getvalue(), stderr.getvalue()

    def test_an_allowed_advisory_also_covers_the_packages_that_depend_on_it(self):
        status, stdout, stderr = self.run_gate(report(PICOMATCH), [entry()])
        self.assertEqual(status, 0, stderr)
        self.assertIn("1 advisories reported, 0 blocking, 1 allowed", stdout)
        self.assertIn("allowed until 2027-01-31: GHSA-fc5q-39xc-vjpf picomatch", stdout)

    def test_an_advisory_outside_the_allowlist_blocks(self):
        status, _, stderr = self.run_gate(report(PICOMATCH), [])
        self.assertEqual(status, 1)
        self.assertIn("blocking (not in the allowlist): GHSA-fc5q-39xc-vjpf picomatch", stderr)

    def test_an_expired_entry_blocks_and_the_last_day_still_counts(self):
        self.assertEqual(self.run_gate(report(PICOMATCH), [entry(expires="2026-11-18")])[0], 0)
        status, _, stderr = self.run_gate(report(PICOMATCH), [entry(expires="2026-11-17")])
        self.assertEqual(status, 1)
        self.assertIn("allowlist entry expired on 2026-11-17", stderr)

    def test_an_entry_covers_only_its_own_advisory_and_package(self):
        other = dict(PICOMATCH, source=1, dependency="axios", name="axios", severity="moderate",
                     url="https://github.com/advisories/GHSA-4hjh-wcwx-xvwj", title="axios advisory", range="<1.0.0")
        status, _, stderr = self.run_gate(report(PICOMATCH, other), [entry()])
        self.assertEqual(status, 1)
        self.assertIn("GHSA-4hjh-wcwx-xvwj axios", stderr)
        self.assertNotIn("GHSA-fc5q-39xc-vjpf", stderr)
        status, _, stderr = self.run_gate(report(PICOMATCH), [entry(package="axios")])
        self.assertEqual(status, 1, "an entry for another package must not cover picomatch")

    def test_a_second_advisory_on_an_allowed_package_blocks(self):
        second = dict(PICOMATCH, source=1250000, severity="moderate", title="another picomatch advisory",
                      url="https://github.com/advisories/GHSA-2xpw-w6gg-jr37", range="<=4.0.7")
        status, stdout, stderr = self.run_gate(report(PICOMATCH, second), [entry()])
        self.assertEqual(status, 1)
        self.assertIn("blocking (not in the allowlist): GHSA-2xpw-w6gg-jr37 picomatch", stderr)
        self.assertNotIn("GHSA-fc5q-39xc-vjpf", stderr)
        self.assertIn("allowed until 2027-01-31: GHSA-fc5q-39xc-vjpf picomatch", stdout)

    def test_repeated_records_of_one_advisory_take_the_highest_severity(self):
        # npm prints one record per npm advisory number, ordered by number,
        # and its advisory cache can keep the severity of an older record.
        older = dict(PICOMATCH, source=2001, severity="low", range="<=4.0.5")
        newer = dict(PICOMATCH, source=2002, severity="high", range=">=4.0.6 <=4.0.7")
        status, _, stderr = self.run_gate(report(older, newer), [])
        self.assertEqual(status, 1)
        self.assertIn("GHSA-fc5q-39xc-vjpf picomatch <=4.0.5 >=4.0.6 <=4.0.7 (high)", stderr)
        self.assertEqual(self.run_gate(report(older, newer), [entry()])[0], 0)

    def test_info_and_low_advisories_never_block(self):
        for severity in ("info", "low"):
            with self.subTest(severity):
                status, stdout, _ = self.run_gate(report(dict(PICOMATCH, severity=severity)), [])
                self.assertEqual(status, 0)
                self.assertIn("1 below moderate", stdout)

    def test_a_missing_or_unknown_severity_blocks(self):
        without = {key: value for key, value in PICOMATCH.items() if key != "severity"}
        for name, advisory in {"missing": without, "upper case": dict(PICOMATCH, severity="HIGH"),
                               "unknown word": dict(PICOMATCH, severity="medium")}.items():
            with self.subTest(name):
                status, _, stderr = self.run_gate(report(advisory), [])
                self.assertEqual(status, 1, stderr)
                self.assertIn("GHSA-fc5q-39xc-vjpf picomatch", stderr)

    def test_an_advisory_without_a_ghsa_id_blocks_under_its_npm_number(self):
        unnamed = dict(PICOMATCH, url="https://www.npmjs.com/advisories/1247710")
        status, _, stderr = self.run_gate(report(unnamed), [entry()])
        self.assertEqual(status, 1)
        self.assertIn("npm-1247710 picomatch", stderr)

    def test_an_entry_that_matches_nothing_is_reported_for_removal(self):
        status, stdout, _ = self.run_gate(report(), [entry()])
        self.assertEqual(status, 0)
        self.assertIn("GHSA-fc5q-39xc-vjpf for picomatch matches no reported advisory; remove it", stdout)

    def test_an_audit_that_failed_or_is_unreadable_fails_closed(self):
        for audit in ({"error": {"code": "ENOAUDIT", "summary": "registry unreachable"}}, {"vulnerabilities": {}}, []):
            with self.subTest(audit=audit):
                self.assertEqual(self.run_gate(audit, [])[0], 2)

    def test_a_report_in_another_shape_fails_closed(self):
        flat_via = report(PICOMATCH)
        flat_via["vulnerabilities"]["picomatch"]["via"] = PICOMATCH
        not_an_object = report(PICOMATCH)
        not_an_object["vulnerabilities"]["picomatch"] = "high"
        odd_cause = report(PICOMATCH)
        odd_cause["vulnerabilities"]["picomatch"]["via"].append(7)
        only_names = report(PICOMATCH)
        only_names["vulnerabilities"]["picomatch"]["via"] = ["fdir"]
        for name, audit in {"via is not a list": flat_via, "entry is not an object": not_an_object,
                            "cause is neither name nor advisory": odd_cause,
                            "npm counts a high vulnerability the gate cannot see": only_names}.items():
            with self.subTest(name):
                status, _, stderr = self.run_gate(audit, [entry()])
                self.assertEqual(status, 2, stderr)

    def test_an_invalid_allowlist_fails_closed(self):
        cases = {
            "malformed id": [entry(advisory_id="CVE-2026-1234")],
            "id with a trailing newline": [entry(advisory_id="GHSA-fc5q-39xc-vjpf\n")],
            "no package": [entry(package=" ")],
            "no reason": [entry(reason="")],
            "bad date": [entry(expires="31.01.2027")],
            "week date": [entry(expires="2027-W05-1")],
            "number as date": [dict(entry(), expires=20270131)],
            "unknown key": [dict(entry(), severity="high")],
            "too far ahead": [entry(expires="2027-05-18")],
            "repeated": [entry(), entry()],
        }
        for name, entries in cases.items():
            with self.subTest(name):
                status, _, stderr = self.run_gate(report(PICOMATCH), entries)
                self.assertEqual(status, 2, stderr)
        self.assertEqual(self.run_gate(report(PICOMATCH), [entry(expires="2027-05-17")])[0], 0,
                         "the last day within the limit is accepted")

    def test_npm_runs_in_the_web_directory_and_its_exit_status_is_ignored(self):
        with tempfile.TemporaryDirectory() as web_dir:
            finished = subprocess.CompletedProcess(["npm"], 1, stdout=json.dumps(report(PICOMATCH)), stderr="")
            with patch.object(gate.subprocess, "run", return_value=finished) as run:
                status, _, stderr = self.run_gate(None, [entry()], extra=("--web-dir", web_dir))
            self.assertEqual(status, 0, stderr)
            run.assert_called_once()
            self.assertEqual(run.call_args.args[0], ["npm", "audit", "--json"])
            self.assertEqual(Path(run.call_args.kwargs["cwd"]), Path(web_dir))

    def test_npm_that_prints_no_report_or_cannot_start_fails_closed(self):
        outcomes = {
            "no output": {"return_value": subprocess.CompletedProcess(["npm"], 1, stdout="", stderr="network down")},
            "error report": {"return_value": subprocess.CompletedProcess(
                ["npm"], 1, stdout=json.dumps({"error": {"code": "E500"}}), stderr="")},
            "npm missing": {"side_effect": FileNotFoundError("npm")},
        }
        for name, outcome in outcomes.items():
            with self.subTest(name), patch.object(gate.subprocess, "run", **outcome):
                self.assertEqual(self.run_gate(None, [entry()])[0], 2)

    def test_the_committed_allowlist_is_valid(self):
        # An entry that is valid today stays within the limit later, and
        # load_allowlist does not refuse expired entries, so this holds over
        # time; the gate itself blocks an expired entry.
        path = gate.ROOT / "web" / "npm-audit-allowlist.json"
        today = datetime.datetime.now(datetime.timezone.utc).date()
        gate.load_allowlist(path.read_text(), today)


if __name__ == "__main__":
    unittest.main()
