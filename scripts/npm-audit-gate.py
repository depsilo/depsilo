#!/usr/bin/env python3
"""
npm audit gate for Depsilo's frontend dependencies.

Runs `npm audit --json` in web/ and fails on every advisory of moderate or
higher severity, as `npm audit --audit-level=moderate` does, except the
advisories listed in web/npm-audit-allowlist.json. Only info and low
advisories never block; a severity the gate does not know blocks too. An
allowlist entry names one advisory (its GHSA ID and the package it affects),
the reason it cannot be fixed yet, and a date after which it no longer
applies, so every exception expires and comes back for review. An entry may
run at most MAX_EXCEPTION_DAYS days past the day of the check.

Only advisories count, not the packages that npm reports because they depend
on an advisory's package: an allowed advisory also covers those. npm can
report one advisory for one package in several records, for example when its
local advisory cache keeps an older severity; the gate takes the highest.

Exit status: 0 when nothing blocks; 1 when an advisory or an expired entry
blocks; 2 when the audit cannot be run or read, or the allowlist is not
valid (a missing allowlist file counts as empty).

Run with: `make security`  or  `python3 scripts/npm-audit-gate.py`.
Tests: `python3 scripts/test-npm-audit-gate.py`.
"""

import argparse
import datetime
import json
from pathlib import Path
import re
import subprocess
import sys

ROOT = Path(__file__).resolve().parent.parent
NON_BLOCKING_SEVERITIES = ("info", "low")
SEVERITY_RANK = {"info": 0, "low": 1, "moderate": 2, "high": 3, "critical": 4}
MAX_EXCEPTION_DAYS = 180
GHSA_ID = re.compile(r"GHSA(-[23456789cfghjmpqrvwx]{4}){3}")
ISO_DATE = re.compile(r"\d{4}-\d{2}-\d{2}")
ENTRY_KEYS = {"id", "package", "reason", "expires"}


class GateError(Exception):
    """The audit report or the allowlist cannot be used."""


def blocks(severity):
    """Whether an advisory of severity blocks unless the allowlist covers it."""
    return severity not in NON_BLOCKING_SEVERITIES


def rank(severity):
    """The order of severities; one the gate does not know ranks highest."""
    return SEVERITY_RANK.get(severity, len(SEVERITY_RANK))


def advisory_id(advisory):
    """The GHSA ID of an npm audit advisory, or its npm source number."""
    url = str(advisory.get("url") or "")
    last = url.rstrip("/").rsplit("/", 1)[-1]
    if GHSA_ID.fullmatch(last):
        return last
    return f"npm-{advisory.get('source', 'unknown')}"


def advisories(report):
    """The distinct advisories of an npm audit --json report (version 2),
    one per GHSA ID and package, each with the highest severity npm gave it."""
    if not isinstance(report, dict) or "error" in report:
        detail = report.get("error") if isinstance(report, dict) else report
        raise GateError(f"npm audit reported an error: {json.dumps(detail)[:500]}")
    vulnerabilities = report.get("vulnerabilities")
    if report.get("auditReportVersion") != 2 or not isinstance(vulnerabilities, dict):
        raise GateError("npm audit output is not an audit report of version 2")
    found = {}
    for name, entry in vulnerabilities.items():
        if not isinstance(entry, dict) or not isinstance(entry.get("via"), list):
            raise GateError(f"npm audit entry {name!r} has no list of causes (via)")
        for via in entry["via"]:
            if isinstance(via, str):
                continue  # the name of another reported package
            if not isinstance(via, dict):
                raise GateError(f"npm audit entry {name!r} has a cause that is neither a package nor an advisory")
            package = str(via.get("dependency") or via.get("name") or name)
            severity = str(via.get("severity") or "unknown")
            key = (advisory_id(via), package)
            advisory = found.setdefault(key, {
                "id": key[0], "package": package, "severity": severity,
                "title": str(via.get("title") or ""), "url": str(via.get("url") or ""), "ranges": [],
            })
            if rank(severity) > rank(advisory["severity"]):
                advisory["severity"] = severity
            advisory_range = str(via.get("range") or "")
            if advisory_range and advisory_range not in advisory["ranges"]:
                advisory["ranges"].append(advisory_range)
    result = sorted(found.values(), key=lambda advisory: (advisory["id"], advisory["package"]))
    check_against_npm_counts(report, result)
    return result


def check_against_npm_counts(report, found):
    """Fail closed when npm reports a vulnerability that blocks but the gate
    found no advisory that does, for example after a change of npm's report
    format."""
    counts = (report.get("metadata") or {}).get("vulnerabilities") or {}
    npm_blocks = any(isinstance(counts.get(severity), int) and counts[severity] > 0
                     for severity in SEVERITY_RANK if blocks(severity))
    npm_blocks = npm_blocks or any(isinstance(entry, dict) and blocks(str(entry.get("severity") or "unknown"))
                                   for entry in report["vulnerabilities"].values())
    if npm_blocks and not any(blocks(advisory["severity"]) for advisory in found):
        raise GateError("npm reports vulnerabilities of moderate or higher severity, but the gate found no such advisory")


def load_allowlist(text, today):
    """Validated allowlist entries keyed by (GHSA ID, package)."""
    try:
        document = json.loads(text)
    except json.JSONDecodeError as error:
        raise GateError(f"allowlist is not JSON: {error}") from error
    entries = document.get("advisories") if isinstance(document, dict) else None
    if not isinstance(entries, list):
        raise GateError('allowlist needs an "advisories" list')
    latest = today + datetime.timedelta(days=MAX_EXCEPTION_DAYS)
    allowed = {}
    for index, entry in enumerate(entries):
        where = f"allowlist entry {index}"
        if not isinstance(entry, dict):
            raise GateError(f"{where} is not an object")
        unknown = sorted(set(entry) - ENTRY_KEYS)
        if unknown:
            raise GateError(f"{where} has unknown keys {unknown}; it takes {sorted(ENTRY_KEYS)}")
        advisory, package, reason, expires = (entry.get(key) for key in ("id", "package", "reason", "expires"))
        if not isinstance(advisory, str) or not GHSA_ID.fullmatch(advisory):
            raise GateError(f"{where} needs an id in the form GHSA-xxxx-xxxx-xxxx")
        if not isinstance(package, str) or not package.strip():
            raise GateError(f"{where} ({advisory}) needs the package it affects")
        if not isinstance(reason, str) or not reason.strip():
            raise GateError(f"{where} ({advisory}) needs a reason")
        if not isinstance(expires, str) or not ISO_DATE.fullmatch(expires):
            raise GateError(f"{where} ({advisory}) needs expires as YYYY-MM-DD")
        try:
            expiry = datetime.date.fromisoformat(expires)
        except ValueError as error:
            raise GateError(f"{where} ({advisory}) needs expires as a real date: {error}") from error
        if expiry > latest:
            raise GateError(f"{where} ({advisory}) expires {expiry}, more than {MAX_EXCEPTION_DAYS} days ahead")
        key = (advisory, package)
        if key in allowed:
            raise GateError(f"{where} repeats {advisory} for {package}")
        allowed[key] = {"expires": expiry, "reason": reason.strip()}
    return allowed


def evaluate(found, allowed, today):
    """Split advisories into blocking, allowed and ignored ones, and list
    allowlist entries that match no reported advisory."""
    blocking, accepted, ignored = [], [], []
    for advisory in found:
        if not blocks(advisory["severity"]):
            ignored.append(advisory)
            continue
        exception = allowed.get((advisory["id"], advisory["package"]))
        if exception is None:
            blocking.append((advisory, "not in the allowlist"))
        elif exception["expires"] < today:
            blocking.append((advisory, f"allowlist entry expired on {exception['expires']}"))
        else:
            accepted.append((advisory, exception))
    reported = {(advisory["id"], advisory["package"]) for advisory in found}
    stale = sorted(key for key in allowed if key not in reported)
    return blocking, accepted, ignored, stale


def run_npm_audit(web_dir):
    """The parsed output of npm audit --json, which exits 1 when it finds
    advisories, so its status alone says nothing."""
    try:
        completed = subprocess.run(["npm", "audit", "--json"], cwd=web_dir, capture_output=True, text=True, check=False)
    except OSError as error:
        raise GateError(f"cannot run npm audit: {error}") from error
    try:
        return json.loads(completed.stdout)
    except json.JSONDecodeError as error:
        raise GateError(f"npm audit printed no JSON report (exit {completed.returncode}): {completed.stderr.strip()[:500]}") from error


def describe(advisory):
    ranges = " ".join(advisory["ranges"])
    return f"{advisory['id']} {advisory['package']} {ranges} ({advisory['severity']}): {advisory['title']}"


def main(argv=None):
    parser = argparse.ArgumentParser(description="Fail on npm advisories that the allowlist does not cover.")
    parser.add_argument("--web-dir", type=Path, default=ROOT / "web", help="directory with package-lock.json")
    parser.add_argument("--allowlist", type=Path, help="default: <web-dir>/npm-audit-allowlist.json")
    parser.add_argument("--report", type=Path, help="read a saved npm audit --json report instead of running npm")
    parser.add_argument("--today", type=datetime.date.fromisoformat, help="YYYY-MM-DD, default: today in UTC")
    args = parser.parse_args(argv)
    today = args.today or datetime.datetime.now(datetime.timezone.utc).date()
    allowlist_path = args.allowlist or args.web_dir / "npm-audit-allowlist.json"

    try:
        report = json.loads(args.report.read_text()) if args.report else run_npm_audit(args.web_dir)
        found = advisories(report)
        allowed = load_allowlist(allowlist_path.read_text() if allowlist_path.exists() else '{"advisories": []}', today)
    except (GateError, OSError, json.JSONDecodeError) as error:
        print(f"npm audit gate: {error}", file=sys.stderr)
        return 2
    except (AttributeError, TypeError, KeyError, ValueError) as error:
        print(f"npm audit gate: the audit report has an unexpected shape: {error!r}", file=sys.stderr)
        return 2

    blocking, accepted, ignored, stale = evaluate(found, allowed, today)
    print(f"npm audit gate: {len(found)} advisories reported, {len(blocking)} blocking, "
          f"{len(accepted)} allowed, {len(ignored)} below moderate")
    for advisory, exception in accepted:
        print(f"allowed until {exception['expires']}: {describe(advisory)}\n  reason: {exception['reason']}")
    for advisory_key in stale:
        print(f"allowlist entry {advisory_key[0]} for {advisory_key[1]} matches no reported advisory; remove it")
    for advisory, why in blocking:
        print(f"blocking ({why}): {describe(advisory)} {advisory['url']}", file=sys.stderr)
    return 1 if blocking else 0


if __name__ == "__main__":
    sys.exit(main())
