#!/usr/bin/env bash
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
extractor="$root/scripts/extract-release-notes.sh"
fixture=$(mktemp -d "${TMPDIR:-/tmp}/depsilo-release-notes-test.XXXXXX")
cleanup() {
    find "$fixture" -depth -delete >/dev/null 2>&1 || true
}
trap cleanup EXIT INT TERM

cat >"$fixture/CHANGELOG.md" <<'EOF'
# Changelog

## [Unreleased]

- future work

## [0.9.1] - 2026-09-01

### Upgrade notes
- stop the old daemon

### Fixed
- release fix

## [0.9.0] - 2026-08-12

- previous release
EOF

notes=$(bash "$extractor" v0.9.1 "$fixture/CHANGELOG.md")
grep -Fq '### Upgrade notes' <<<"$notes"
grep -Fq -- '- stop the old daemon' <<<"$notes"
grep -Fq -- '- release fix' <<<"$notes"
if grep -Fq 'future work' <<<"$notes" || grep -Fq 'previous release' <<<"$notes"; then
    echo 'release notes crossed a changelog section boundary' >&2
    exit 1
fi

if bash "$extractor" v0.9.2 "$fixture/CHANGELOG.md" >/dev/null 2>&1; then
    echo 'release notes extractor accepted a missing version section' >&2
    exit 1
fi

actual_notes=$(bash "$extractor" v0.9.1 "$root/CHANGELOG.md")
for required in \
    'unauthenticated PID-only record' \
    'UID/GID `10001:10001`' \
    'DEPSILO_ACCEPT_JWT_ROTATION=1' \
    '`s3:AbortMultipartUpload`' \
    'Xet read-token routes' \
    '`depsilo doctor`' \
    'environment-only'; do
    grep -Fq "$required" <<<"$actual_notes" || {
        echo "v0.9.1 release notes omit: $required" >&2
        exit 1
    }
done

echo 'release notes extraction tests passed'
