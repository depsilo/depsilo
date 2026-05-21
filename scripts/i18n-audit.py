#!/usr/bin/env python3
"""
i18n consistency audit for Depsilo's frontend.

Walks `web/src/` and reports:

  1. Keys referenced via `t('ns.key')` in TSX/TS but NOT defined in
     either zh.ts or en.ts  (these render as raw key strings in the UI).
  2. Keys defined in zh.ts but missing from en.ts  (translation drift).
  3. Keys defined in en.ts but missing from zh.ts  (translation drift).
  4. Duplicate keys within the same namespace in zh.ts / en.ts.
  5. `{{placeholder}}` mismatch across locales — a translator dropping
     or renaming a variable silently breaks interpolation in only one
     language, which is hard to spot manually.

Exits non-zero when any of the five buckets is non-empty, so it can
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


_VALUE_PAT = re.compile(
    r"""^\s*([a-zA-Z_][a-zA-Z0-9_]*)\s*:\s*(['"])((?:\\.|(?!\2).)*)\2"""
)


def parse_locale(path: str) -> tuple[set[str], list[tuple[str, int]], dict[str, str]]:
    """Parse the locale TS file and return (defined_keys, duplicates, values).

    `duplicates` is a list of (key_path, line_number) for keys declared
    more than once within the same namespace — TypeScript catches these
    at build time (TS1117), but reporting them here turns the lint into
    a single-stop pre-flight check.

    `values` maps the full dotted key path to its raw string value, used
    downstream for placeholder consistency checks across locales.
    """
    keys: set[str] = set()
    duplicates: list[tuple[str, int]] = []
    values: dict[str, str] = {}
    # Track keys per parent namespace path to detect siblings with same name.
    seen_per_ns: dict[tuple[str, ...], set[str]] = {}
    stack: list[str] = []
    with open(path, encoding="utf-8") as f:
        for lineno, line in enumerate(f, start=1):
            stripped = line.rstrip()
            if re.match(r"^\s*\},?\s*$", stripped):
                if stack:
                    stack.pop()
                continue
            m = re.match(r"^\s*([a-zA-Z_][a-zA-Z0-9_]*)\s*:\s*\{\s*$", stripped)
            if m:
                ns = m.group(1)
                parent = tuple(stack)
                if ns in seen_per_ns.get(parent, set()):
                    duplicates.append((".".join(stack + [ns]), lineno))
                seen_per_ns.setdefault(parent, set()).add(ns)
                stack.append(ns)
                continue
            m = _VALUE_PAT.match(stripped)
            if m:
                k = m.group(1)
                parent = tuple(stack)
                if k in seen_per_ns.get(parent, set()):
                    duplicates.append((".".join(stack + [k]), lineno))
                seen_per_ns.setdefault(parent, set()).add(k)
                full = ".".join(stack + [k])
                keys.add(full)
                values[full] = m.group(3)
    return keys, duplicates, values


def _common_wrapper(keys: set[str]) -> str:
    """Return the longest prefix (up to 2 levels, dot-terminated) shared
    by every key — or '' if there's no common wrapper to strip.
    """
    if not keys:
        return ""
    sample = next(iter(keys))
    parts = sample.split(".")
    for n in (2, 1):
        prefix = ".".join(parts[:n]) + "."
        if all(k.startswith(prefix) for k in keys):
            return prefix
    return ""


def strip_wrapper(keys: set[str]) -> set[str]:
    """If every key starts with `resources.` (or `resources.zh.` etc),
    strip that wrapper so paths match what callers use in `t(...)`.
    """
    prefix = _common_wrapper(keys)
    return {k[len(prefix):] for k in keys} if prefix else keys


def strip_wrapper_values(values: dict[str, str]) -> dict[str, str]:
    """Apply the same wrapper strip to a values dict, keyed by full path."""
    prefix = _common_wrapper(set(values.keys()))
    if not prefix:
        return values
    return {k[len(prefix):]: v for k, v in values.items()}


_PLACEHOLDER_PAT = re.compile(r"\{\{\s*([^}\s]+?)\s*(?:,[^}]*)?\}\}")


def placeholders(s: str) -> frozenset[str]:
    """Extract the set of `{{var}}` placeholder names from a translation
    string. Strips i18next formatting suffixes like `{{count, number}}`.
    """
    return frozenset(_PLACEHOLDER_PAT.findall(s))


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

    zh_raw, zh_dups, zh_vals_raw = parse_locale(zh_path)
    en_raw, en_dups, en_vals_raw = parse_locale(en_path)
    zh = strip_wrapper(zh_raw)
    en = strip_wrapper(en_raw)
    zh_vals = strip_wrapper_values(zh_vals_raw)
    en_vals = strip_wrapper_values(en_vals_raw)
    used = collect_used([WEB_SRC])

    missing = sorted(set(used.keys()) - zh - en)
    zh_only = sorted(zh - en)
    en_only = sorted(en - zh)

    placeholder_mismatches: list[tuple[str, frozenset[str], frozenset[str]]] = []
    for key in sorted(zh & en):
        zh_ph = placeholders(zh_vals.get(key, ""))
        en_ph = placeholders(en_vals.get(key, ""))
        if zh_ph != en_ph:
            placeholder_mismatches.append((key, zh_ph, en_ph))

    print("i18n audit")
    print(f"  files scanned: TSX/TS under {os.path.relpath(WEB_SRC, REPO_ROOT)}")
    print(f"  zh.ts:   {len(zh)} keys")
    print(f"  en.ts:   {len(en)} keys")
    print(f"  used:    {len(used)} distinct dot-keys via t(...)")
    print()

    if zh_dups:
        print(f"DUPLICATE KEYS IN zh.ts  ({len(zh_dups)}):")
        for k, ln in zh_dups:
            print(f"  - {k}  (line {ln})")
        print()
    if en_dups:
        print(f"DUPLICATE KEYS IN en.ts  ({len(en_dups)}):")
        for k, ln in en_dups:
            print(f"  - {k}  (line {ln})")
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
    if placeholder_mismatches:
        print(f"PLACEHOLDER MISMATCH  ({len(placeholder_mismatches)}):")
        for k, zh_ph, en_ph in placeholder_mismatches:
            zh_only_ph = sorted(zh_ph - en_ph)
            en_only_ph = sorted(en_ph - zh_ph)
            diff_bits = []
            if zh_only_ph:
                diff_bits.append(f"only in zh: {{{', '.join(zh_only_ph)}}}")
            if en_only_ph:
                diff_bits.append(f"only in en: {{{', '.join(en_only_ph)}}}")
            print(f"  - {k}    [{'; '.join(diff_bits)}]")
        print()

    if missing or zh_only or en_only or zh_dups or en_dups or placeholder_mismatches:
        return 1
    print("OK — all keys defined in both locales, no duplicates, all defined keys are used safely.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
