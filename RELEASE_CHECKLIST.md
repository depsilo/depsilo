# Release Checklist

## Pre-release

### Code Quality
- [ ] `go vet ./...` passes
- [ ] `go test ./... -race` passes
- [ ] `make test-integration` passes
- [ ] `cd web && npm run type-check` succeeds
- [ ] `cd web && npm run build` succeeds
- [ ] i18n audit passes: `python3 scripts/i18n-audit.py`

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

### Docker
- [ ] Docker image pushed to Docker Hub (`depsilo/depsilo`)
- [ ] Docker image pushed to GHCR (`ghcr.io/depsilo/depsilo`)
- [ ] Stable release tags published: `X.Y.Z`, `X.Y`, `X`, and metadata-action's automatic `latest`
- [ ] GHCR package is public and an anonymous `docker pull ghcr.io/depsilo/depsilo:X.Y.Z` succeeds

### Post-release
- [ ] `curl -fsSL https://depsilo.com/install.sh | bash` works on clean machine
- [ ] `docker run depsilo/depsilo:latest` starts successfully
- [ ] Release notes published on GitHub Releases

## Known release gaps

- Release binaries, checksums, container images, and SBOMs are currently unsigned;
  cosign keyless signing and attestations are not automated yet.
- Homebrew formula generation runs locally in GoReleaser, but tap publishing is
  disabled with `skip_upload: true` until `depsilo/homebrew-tap` exists.
- There is no Windows NSIS/tray installer job; Windows CLI zip archives are published.
- The GHCR package currently requires authentication; Docker Hub is the anonymous pull path.
