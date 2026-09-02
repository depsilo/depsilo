#!/usr/bin/env bash
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)

# The direct-predecessor source contract is intentionally locked here rather
# than inherited from a caller. The shared source-state implementation also
# remains the long-range v0.9.0 contract behind make test-v090-upgrade.
DEPSILO_UPGRADE_BASELINE_TAG=v0.9.1 \
DEPSILO_UPGRADE_BASELINE_COMMIT=773b9ad673615d5df6a8281f7cb658e3df84527d \
  bash "$root/scripts/test-v090-upgrade.sh"

# Source compatibility is necessary but not sufficient: users ran the
# published image, whose entrypoint, uid/gid, default paths, and named volume
# are also part of the upgrade interface.
bash "$root/scripts/test-v091-image-upgrade.sh"
