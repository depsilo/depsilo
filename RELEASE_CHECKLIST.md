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
- [ ] `make test-e2e` passes with all 14 official package-manager clients
- [ ] `make test-docker-docker` passes with the Docker Registry client and dind
- [ ] `make test-compiler-cache-qualified` passes with pinned ccache and sccache
- [ ] `make test-s3` passes against the pinned MinIO image
- [ ] `make test-v090-upgrade` reopens v0.9.0 state and preserves config,
      SQLite identities, password/JWT/API credentials, and legacy npm cache
      records; it rejects unsigned cached npm data while offline, then refetches
      online metadata and proves fresh source-bound signed artifact provenance
- [ ] `make test-v090-compose-upgrade` upgrades the immutable published v0.9.0
      image's exact shipped bind layout, explicitly rotates a weak legacy JWT
      secret, preserves passwords, API tokens, database, license and legacy
      npm cache records, rejects unsigned legacy npm artifacts, establishes fresh
      source-bound signed provenance after reconnect, and proves Admin Settings
      can atomically rewrite the prepared candidate config
- [ ] `make test-v091-upgrade` upgrades the fixed direct-predecessor source and
      immutable published v0.9.1 image. It reopens the shipped named-volume
      state with the candidate, preserves config, password/JWT/API credentials,
      trial and paid entitlement, and migrates a safe ecosystem-wide Package
      Rule to schema v3 with dialect revision 1

### Binary
- [ ] `make build` produces `bin/depsilo` successfully
- [ ] `./bin/depsilo version` shows correct version
- [ ] `./bin/depsilo serve` starts without errors
- [ ] `./bin/depsilo backup --out /tmp/test.tar.gz` works

### Release Artifacts
- [ ] `install.sh` is executable and bash-syntax valid
- [ ] `config.example.toml` has no real secrets
- [ ] `CHANGELOG.md` updated with new version entry
- [ ] `goreleaser check` passes: `make release-check`

## Release

### Tag & Push
```bash
git tag -a vX.Y.Z -m "vX.Y.Z"
git push origin vX.Y.Z
```

### Automated (GitHub Actions)
- [ ] Release qualification finishes before any release asset is published
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
- [ ] `curl -fsSL https://depsilo.com/install.sh | bash` works on clean machine
- [ ] `docker run ghcr.io/depsilo/depsilo:latest` starts successfully
- [ ] Release notes published on GitHub Releases

## Known release gaps

- Homebrew is not generated or published by the current GoReleaser config; the
  legacy formula is not a supported delivery channel.
- There is no Windows NSIS/tray installer job; Windows CLI zip archives are published.
