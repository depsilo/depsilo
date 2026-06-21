# Release Checklist

## Pre-release

### Code Quality
- [ ] `go vet ./...` passes
- [ ] `go test ./...` all tests pass
- [ ] `make test-unit` + `make test-integration` pass
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
- [ ] Homebrew formula auto-updated in `depsilo/homebrew-tap`
- [ ] Desktop builds: macOS (.app), Windows (NSIS installer), Linux (.tar.gz)

### Docker
- [ ] Docker image pushed to Docker Hub (`depsilo/depsilo`)
- [ ] Docker image pushed to GHCR (`ghcr.io/depsilo/depsilo`)
- [ ] Tags: `latest`, `X.Y.Z`, `X.Y`, `X`

### Post-release
- [ ] `curl -fsSL https://depsilo.com/install.sh | bash` works on clean machine
- [ ] `brew install depsilo/tap/depsilo` works on macOS
- [ ] `docker run depsilo/depsilo:latest` starts successfully
- [ ] Release notes published on GitHub Releases
