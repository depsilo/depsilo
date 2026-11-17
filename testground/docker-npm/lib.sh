# Shell helpers for the RUN steps of the Dockerfile in this directory.
# Each step sources this file, so a failed check stops the build with one line
# that says what was expected.

fail() {
    echo "FAILED: $*" >&2
    exit 1
}

# cold_start points HOME at a new empty directory. Every client in this fixture
# keeps its download cache or package store below HOME and reads its registry
# from a file in the project directory, so the command that follows starts with
# an empty client cache and has to fetch from Depsilo. That holds only for a
# user other than root: as root, Yarn 1 keeps its cache below /usr/local/share
# whatever HOME says, and a later step would install from it without a request.
cold_start() {
    [ "$(id -u)" != 0 ] \
        || fail "cold_start ran as root; Yarn 1 ignores HOME for root, so the client cache would not be empty"
    HOME=$(mktemp -d)
    export HOME
}

# through_depsilo LOCKFILE fails unless the lockfile records a tarball URL that
# Depsilo issued. A client that ignored its registry setting installs from the
# public registry without an error and records that registry instead.
through_depsilo() {
    grep -q '__depsilo_tarball_v1' "$1" \
        || fail "$1 records no tarball URL issued by Depsilo; the client did not install through ${DEPSILO_NPM}"
}

# lodash_loads [RUNNER] fails unless Node loads the installed lodash 4.17.21.
# RUNNER starts Node for a client that installs without node_modules.
lodash_loads() {
    [ "$("$@" node -p "require('lodash').VERSION")" = 4.17.21 ] \
        || fail "lodash 4.17.21 does not load after the install"
}

# unsigned_tarball_refused LOG TARBALL_PATH checks a failed npm run. LOG must
# report a 404 for the conventional tarball path below the registry and repeat
# the remedy Depsilo sends. npm prints the "error" text of the response and not
# its code, so the code is read from a direct request for the same path.
unsigned_tarball_refused() {
    grep -q "404 Not Found - GET ${DEPSILO_NPM}$2" "$1" \
        || fail "npm did not report a 404 for ${DEPSILO_NPM}$2"
    grep -q -e '--omit-lockfile-registry-resolved' "$1" \
        || fail "the npm output does not name the lockfile rewrite Depsilo recommends"
    node -e '
        fetch(process.argv[1]).then(async (response) => {
            const body = await response.json()
            if (response.status !== 404 || body.code !== "NPM_UNSIGNED_TARBALL") {
                console.error(response.status, JSON.stringify(body))
                process.exit(1)
            }
        })' "${DEPSILO_NPM}$2" \
        || fail "${DEPSILO_NPM}$2 did not answer 404 with the code NPM_UNSIGNED_TARBALL"
}

# without_resolved_urls LOCKFILE fails if package-lock.json still pins tarball
# URLs. Without them npm asks the configured registry where each tarball is.
without_resolved_urls() {
    if grep -q '"resolved"' "$1"; then
        fail "$1 still records resolved URLs after the rewrite"
    fi
}
