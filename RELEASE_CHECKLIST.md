# Release Checklist — v0.1.0

## Code Quality

- [ ] `go vet ./...` passes with no errors
- [ ] `go test ./...` all tests pass
- [ ] Frontend TypeScript has no type errors (`npm run type-check`)
- [ ] Frontend builds successfully (`npm run build`)
- [ ] No unresolved TODO/FIXME/HACK comments in critical paths

## Security

- [ ] No hardcoded private IP addresses / real secrets in committed code
- [ ] `.gitignore` covers: `config.toml`, `.env`, `*.db`, `*.sqlite`, `data/`, `bin/`, `web/dist/`, `*.log`
- [ ] `config.example.toml` contains only placeholder values
- [ ] JWT secret has forced-change warning at startup and in config comments
- [ ] Default admin password (`admin:admin`) has change warning at startup
- [ ] Git history clean of sensitive files (`config.toml` removed via filter-branch)
- [ ] `SECURITY.md` exists with vulnerability reporting instructions

## Documentation

- [ ] `README.md` complete — Quick Start commands can be copy-pasted and run
- [ ] `config.example.toml` has comments explaining every field
- [ ] `docker-compose.yml` works out of the box with minimal setup
- [ ] `.env.example` documents all environment variables
- [ ] `CONTRIBUTING.md` exists with dev setup and PR guidelines
- [ ] `CHANGELOG.md` exists with v0.1.0 entry
- [ ] `LICENSE` file present (MIT)

## GitHub Repository Settings

- [ ] Repository description filled (e.g., "Lightweight pip/apt caching proxy gateway written in Go")
- [ ] Topics added: `go`, `docker`, `pip`, `apt`, `cache`, `proxy`, `self-hosted`, `mirror`
- [ ] License set to MIT
- [ ] About section: website URL pointing to landing page
- [ ] GitHub Actions CI workflow (`.github/workflows/ci.yml`) passing on main
- [ ] Branch protection on `main`: require PR reviews, require CI pass
- [ ] Issue templates configured (bug report + feature request)
- [ ] PR template configured
- [ ] Disable unused features (Projects / Wiki — optional)

## Release

- [ ] Version string in code matches release tag (v0.1.0)
- [ ] Git tag created: `git tag -a v0.1.0 -m "Initial release"`
- [ ] GitHub Release created with release notes
- [ ] Docker image built and pushed to Docker Hub
- [ ] Docker image tags: `latest` + `v0.1.0`
- [ ] Landing page URL accessible
- [ ] Landing page GitHub link points to correct repository
- [ ] Quick Start instructions tested on a clean machine
