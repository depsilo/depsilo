# Release Checklist

## Pre-release

### Code Quality
- [ ] `make verify` passes (lint, race, integration, build, browser, scripts)
- [ ] `make security` passes (online dependency vulnerability scan)

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

- Homebrew formula generation runs locally in GoReleaser, but tap publishing is
  disabled with `skip_upload: true` until `depsilo/homebrew-tap` exists.
- There is no Windows NSIS/tray installer job; Windows CLI zip archives are published.
