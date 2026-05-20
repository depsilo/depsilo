#!/usr/bin/env python3
"""
i18n consistency audit for Depsilo's frontend.

Walks `web/src/` and reports:

  1. Keys referenced via `t('ns.key')` in TSX/TS but NOT defined in
     either zh.ts or en.ts  (these render as raw key strings in the UI).
  2. Keys defined in zh.ts but missing from en.ts  (translation drift).
  3. Keys defined in en.ts but missing from zh.ts  (translation drift).

Exits non-zero when any of the three buckets is non-empty, so it can
gate CI / pre-commit.

Run with: `make lint-i18n`  or  `python3 scripts/i18n-audit.py`.
"""
from __future__ import annotations

import glob
import os
import re
import sys

# Resolve `web/src/` relative to this script — works regardless of cwd.
SCRIPT_DIR = os.path.dirname(os.path.abspath(__file__))
REPO_ROOT = os.path.dirname(SCRIPT_DIR)
WEB_SRC = os.path.join(REPO_ROOT, "web", "src")


def parse_locale(path: str) -> set[str]:
    """Return the set of dot-keyed leaf paths defined in a TS object literal.

    The parser is intentionally naive: it tracks a stack of namespace
    names by detecting `<word>: {` open lines and `},?` close lines.
    Leaf entries are `<word>: '...'` (or `"..."`). This is enough for
    the hand-written zh.ts / en.ts in this repo.
    """
    keys: set[str] = set()
    stack: list[str] = []
    with open(path, encoding="utf-8") as f:
        for line in f:
            stripped = line.rstrip()
            # close brace pops a namespace
            if re.match(r"^\s*\},?\s*$", stripped):
                if stack:
                    stack.pop()
                continue
            # `key: {`  ->  push namespace
            m = re.match(r"^\s*([a-zA-Z_][a-zA-Z0-9_]*)\s*:\s*\{\s*$", stripped)
            if m:
                stack.append(m.group(1))
                continue
            # `key: '...'`  ->  leaf
            m = re.match(r"""^\s*([a-zA-Z_][a-zA-Z0-9_]*)\s*:\s*['"]""", stripped)
            if m:
                keys.add(".".join(stack + [m.group(1)]))
    return keys


def strip_wrapper(keys: set[str]) -> set[str]:
    """If every key starts with `resources.` (or `resources.zh.` etc),
    strip that wrapper so paths match what callers use in `t(...)`.
    """
    if not keys:
        return keys
    sample = next(iter(keys))
    parts = sample.split(".")
    for n in (2, 1, 0):
        if n == 0:
            return keys
        prefix = ".".join(parts[:n]) + "."
        if all(k.startswith(prefix) for k in keys):
            return {k[len(prefix):] for k in keys}
    return keys


def collect_used(roots: list[str]) -> dict[str, list[str]]:
    """Return {key: [files...]} for every `t('ns.key')` call found."""
    used: dict[str, list[str]] = {}
    pat = re.compile(r"""\bt\(\s*['"]([a-zA-Z_][a-zA-Z0-9_.]*)['"]""")
    for root in roots:
        for pattern in ("**/*.tsx", "**/*.ts"):
            for path in glob.glob(os.path.join(root, pattern), recursive=True):
                with open(path, encoding="utf-8") as f:
                    src = f.read()
                for m in pat.finditer(src):
                    key = m.group(1)
                    if "." not in key:
                        # Skip top-level shortcuts like t('loading')
                        continue
                    used.setdefault(key, []).append(os.path.relpath(path, WEB_SRC))
    return used


def main() -> int:
    zh_path = os.path.join(WEB_SRC, "i18n", "zh.ts")
    en_path = os.path.join(WEB_SRC, "i18n", "en.ts")
    if not os.path.exists(zh_path) or not os.path.exists(en_path):
        print(f"!! locale files not found under {WEB_SRC}/i18n/", file=sys.stderr)
        return 2

    zh = strip_wrapper(parse_locale(zh_path))
    en = strip_wrapper(parse_locale(en_path))
    used = collect_used([WEB_SRC])

    missing = sorted(set(used.keys()) - zh - en)
    zh_only = sorted(zh - en)
    en_only = sorted(en - zh)

    print("i18n audit")
    print(f"  files scanned: TSX/TS under {os.path.relpath(WEB_SRC, REPO_ROOT)}")
    print(f"  zh.ts:   {len(zh)} keys")
    print(f"  en.ts:   {len(en)} keys")
    print(f"  used:    {len(used)} distinct dot-keys via t(...)")
    print()

    if missing:
        print(f"USED BUT UNDEFINED  ({len(missing)}):")
        for k in missing:
            files = sorted(set(used[k]))
            print(f"  - {k}    [{', '.join(files)}]")
        print()
    if zh_only:
        print(f"IN zh.ts BUT NOT en.ts  ({len(zh_only)}):")
        for k in zh_only:
            print(f"  - {k}")
        print()
    if en_only:
        print(f"IN en.ts BUT NOT zh.ts  ({len(en_only)}):")
        for k in en_only:
            print(f"  - {k}")
        print()

    if missing or zh_only or en_only:
        return 1
    print("OK — all keys defined in both locales, all defined keys are used safely.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
