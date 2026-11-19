#!/usr/bin/env python3
"""Write a copy of a config whose upstreams are tried in reverse priority.

The example config puts regional mirrors first and the official registry
second. CI runners sit outside mainland China, where those mirrors can be slow
enough to time a real-client test out, so `make test-e2e E2E_ORIGIN_FIRST=1`
tries the official registries first. Usage: e2e-origin-first.py SRC DST
"""
import re
import sys

source, target = sys.argv[1], sys.argv[2]
lines = open(source, encoding="utf-8").read().split("\n")
section = re.compile(r"^\[\[([a-z]+)\.upstreams\]\]")
priority = re.compile(r"^(priority\s*=\s*)(\d+)(.*)$")

groups = {}  # ecosystem -> [(line index, priority)]
current = None
for index, line in enumerate(lines):
    if line.startswith("["):
        match = section.match(line)
        current = match.group(1) if match else None
        continue
    match = priority.match(line)
    if current and match:
        groups.setdefault(current, []).append((index, int(match.group(2))))

for entries in groups.values():
    values = sorted(value for _, value in entries)
    reversed_value = dict(zip(values, reversed(values)))
    for index, value in entries:
        match = priority.match(lines[index])
        lines[index] = f"{match.group(1)}{reversed_value[value]}{match.group(3)}"

open(target, "w", encoding="utf-8").write("\n".join(lines))
