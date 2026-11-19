# Release Checklist

## Pre-release

### Code Quality
- [ ] `make verify` passes (lint, race, integration, build, browser, scripts)
- [ ] First-run setup durability matrix passes: database commit precedes config
      publication, and every injected boundary reopens Setup or preserves admin
      login after a clean restart
- [ ] `make security` passes (online dependency vulnerability scan)
- [ ] `make test-ui-production` passes against the embedded production frontend
- [ ] The production container runs as UID/GID `10001:10001` and `/ready`
      succeeds with its single state volume

### Release qualification
- [ ] No other Depsilo instance holds the qualification port. On platforms
      that allow a wildcard bind next to an existing loopback bind (macOS),
      containers reach the older instance; run `make stop` first. The service
      manager now fails the start instead of reporting success when another
      listener shares the port.
- [ ] `make test-e2e` passes with all 14 official package-manager clients
- [ ] `make test-docker-docker` passes with the Docker Registry client and dind
- [ ] `make test-compiler-cache-qualified` passes with pinned ccache and sccache
- [ ] `make test-s3` passes against the pinned S3 contract image (RustFS)
- [ ] `make test-previous-release-upgrade` upgrades the previous published
      release. It runs that release's image by digest on its shipped Compose
      named-volume layout, completes first-run setup through the API, and
      starts the candidate on the same volume. The candidate leaves
      `config.toml` byte-identical, does not reopen first-run setup, honors
      the session issued before the upgrade, both passwords, and the API
      token, keeps the Package Rule, and reaches its own schema version. The
      contract fails when its pinned baseline is older than the newest
      release in the candidate's history

### Rollback rehearsal
- [ ] `bin/depsilo backup --out <file>.tar.gz --json` reports `ok`, and the
      archive contains exactly `config.toml`, `depsilo.db`, and `manifest.json`
- [ ] `bin/depsilo restore <file>.tar.gz --config-target <dir>/config.toml
      --database-target <dir>/data/depsilo.db` writes both files with mode `600`
      into a fresh directory while the original server is stopped
- [ ] the restored state starts, `/ready` succeeds, and the administrator
      created before the backup can log in

### Binary
- [ ] `make build` produces `bin/depsilo` successfully
- [ ] `./bin/depsilo version` shows correct version
- [ ] `./bin/depsilo serve` starts without errors
- [ ] `./bin/depsilo backup --out /tmp/test.tar.gz` works

### Release Artifacts
- [ ] `install.sh` is executable and bash-syntax valid
- [ ] `config.example.toml` has no real secrets
- [ ] `SECURITY.md` still holds. This release is on the latest minor release
      line, the only line its supported-versions rule covers; if it is a
      patch for an older line, update that rule in `SECURITY.md` and
      `docs/compatibility.md` first. GitHub private vulnerability reporting
      is enabled:
      `gh api repos/depsilo/depsilo/private-vulnerability-reporting` returns
      `{"enabled":true}`
- [ ] `CHANGELOG.md` updated with new version entry
- [ ] `goreleaser check` passes: `make release-check`

## Release

### Tag & Push
Push the release commit to `master` and wait for CI; dispatch release
qualification on it and wait for it to pass. The release workflow checks both
runs for the tagged commit and does not repeat them.
```bash
gh workflow run real-client-e2e.yml --ref master -f release_qualification=true
git tag -a vX.Y.Z -m "vX.Y.Z"
git push origin vX.Y.Z
```

### Automated (GitHub Actions)
- [ ] The qualified-commit gate finds passed CI and release-qualification runs for the tag
- [ ] Native Windows and macOS detached-daemon lifecycle checks pass
- [ ] The promotion lock verifies the authoritative tag still resolves to the
      triggering commit and that its floating-tag decision remains current
- [ ] GoReleaser builds CLI binaries for 6 platforms
- [ ] CLI archives uploaded (linux/darwin/windows × amd64/arm64)
- [ ] Checksums uploaded
- [ ] `install.sh` uploaded to release
- [ ] Source SBOMs uploaded: CycloneDX + SPDX
- [ ] Container-image SBOMs uploaded: CycloneDX + SPDX
- [ ] Tray builds uploaded: macOS (`.app` zip) and Linux amd64 (`.tar.gz`)
- [ ] Linux amd64 archive smoke test and `checksums.txt` verification pass
- [ ] Sigstore bundles uploaded for checksums, installer, tray bundles, and SBOMs

### Docker
- [ ] Canonical image pushed to GHCR (`ghcr.io/depsilo/depsilo`)
- [ ] Mirror image pushed to Docker Hub (`depsilo/depsilo`)
- [ ] Stable release tags published: `X.Y.Z`, `X.Y`, `latest`, and `X` for 1.x or newer
- [ ] GHCR package is public and an anonymous `docker pull ghcr.io/depsilo/depsilo:X.Y.Z` succeeds
- [ ] Both registry digests have keyless signatures and CycloneDX attestations
- [ ] Published image starts and `/ready` succeeds in the automated smoke test

### Post-release
- [ ] `curl -fsSL https://depsilo.com/install.sh | bash` works on a clean
      machine on a network other than the maintainer's own, for example a
      cloud VM or a CI runner. The installer URL can answer one network with
      the script and another with a bot challenge, so a pass from the
      maintainer's network does not show what a new user gets. A good answer
      is the script itself:
      `curl -fsSL https://depsilo.com/install.sh | head -1` prints
      `#!/usr/bin/env bash`. The one-liner must resolve the newest tag
      without the rate-limited REST API, and the pinned form
      (`DEPSILO_VERSION=vX.Y.Z`) must install the release being verified.
      `https://depsilo.com/install.sh` is a redirect to the `install.sh`
      asset of the latest release, so installer fixes ship with the next
      release
- [ ] `docker run ghcr.io/depsilo/depsilo:latest` starts successfully
- [ ] Release notes published on GitHub Releases
- [ ] `scripts/test-previous-release-upgrade.sh` is re-pinned to the release
      just published (tag commit, Compose checksum, image index and
      per-architecture digests; see "Re-pinning the previous-release
      contract" in `docs/release-verification.md`), and
      `make test-previous-release-upgrade` passes with the new baseline. Until
      then the next release qualification fails on the stale baseline

## Known release gaps

- Homebrew is not generated or published by the current GoReleaser config; the
  legacy formula is not a supported delivery channel.
- There is no Windows NSIS/tray installer job; Windows CLI zip archives are published.
