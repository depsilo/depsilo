#!/usr/bin/env bash
set -euo pipefail

tag="${1:-}"
if [[ "$tag" != v* ]]; then
    echo "release tag must start with v: $tag" >&2
    exit 1
fi

# SemVer 2.0.0: numeric core and prerelease identifiers may not contain
# leading zeroes; alphanumeric prerelease/build identifiers use the allowed
# ASCII character set only.
identifier='(0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*)'
prerelease="${identifier}(\.${identifier})*"
build='[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*'
semver="^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-${prerelease})?(\+${build})?$"

if [[ ! "${tag#v}" =~ $semver ]]; then
    echo "release tag must be valid SemVer with a v prefix: $tag" >&2
    exit 1
fi
