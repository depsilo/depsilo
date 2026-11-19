#!/usr/bin/env bash
# Gate a release on checks that already passed for the exact commit, instead
# of running them again: the CI workflow (offline verification) and a
# release-qualification run of the real-client workflow (dispatched with
# release_qualification=true). Needs gh with GH_TOKEN and actions:read.
#
# Usage: check-release-qualification.sh <repository> <commit sha>
set -euo pipefail

repository=${1:?repository, for example depsilo/depsilo}
sha=${2:?commit sha}

# Jobs that run only in release qualification; all must have succeeded in
# one run for the commit.
required_jobs=(
    "All 14 ecosystems"
    "Docker OCI client"
    "ccache and sccache clients"
    "S3 storage contract"
    "Previous-release upgrade contract"
)

successful_runs() {
    gh api "repos/$repository/actions/workflows/$1/runs?head_sha=$sha&status=success&per_page=20" \
        --jq '.workflow_runs[].id'
}

ci_runs=$(successful_runs ci.yml)
if [ -z "$ci_runs" ]; then
    echo "No successful CI run for $sha. Push the commit to master (or run the CI workflow on it) and wait for it to pass." >&2
    exit 1
fi
echo "CI passed for $sha (run $(head -1 <<<"$ci_runs"))."

for run in $(successful_runs real-client-e2e.yml); do
    passed=$(gh api "repos/$repository/actions/runs/$run/jobs?per_page=50" \
        --jq '.jobs[] | select(.conclusion == "success") | .name')
    missing=0
    for job in "${required_jobs[@]}"; do
        grep -Fqx "$job" <<<"$passed" || missing=1
    done
    if [ "$missing" -eq 0 ]; then
        echo "Release qualification passed for $sha (run $run)."
        exit 0
    fi
done
echo "No successful release-qualification run for $sha. Run:" >&2
echo "  gh workflow run real-client-e2e.yml --ref <branch> -f release_qualification=true" >&2
echo "on a branch whose head is $sha, wait for it to pass, then re-run this release." >&2
exit 1
