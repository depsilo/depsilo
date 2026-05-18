# Contributing to Depsilo

Contributions are welcome! Whether it's a bug fix, new feature, or documentation improvement — we appreciate your help.

## Development Environment

### Prerequisites

- Go 1.21+
- Node.js 20+
- npm 9+
- Make

### Setup

```bash
# Clone the repository
git clone https://github.com/depsilo/depsilo.git
cd depsilo

# Install Go dependencies
go mod download

# Install frontend dependencies
cd web && npm install && cd ..

# Copy example config
cp config.example.toml config.toml
```

### Running Locally

**Option 1: Full dev environment**
```bash
make dev
```

**Option 2: Separate terminals**
```bash
# Terminal 1 — Backend
go run ./cmd/server

# Terminal 2 — Frontend (hot reload)
cd web && npm run dev
```

The frontend dev server runs on `http://localhost:5173` and proxies API requests to the backend on `:8080`.

## Submitting a Pull Request

1. **Fork** the repository
2. **Create a branch** from `master`: `git checkout -b feat/my-feature`
3. **Make your changes** and commit using [Conventional Commits](https://www.conventionalcommits.org/):
   ```
   feat: add support for conda repositories
   fix: handle upstream timeout gracefully
   docs: update Quick Start instructions
   refactor: extract cache key generation
   ```
4. **Push** to your fork: `git push origin feat/my-feature`
5. **Open a Pull Request** against `master`

### PR Guidelines

- Keep PRs focused — one feature or fix per PR
- Update documentation if behavior changes
- Add tests for new functionality
- Fill out the PR template completely

## Reporting Issues

### Bug Reports

Please include:
- Depsilo version (`depsilo --version` or Docker image tag)
- Deployment method (binary / Docker / docker-compose)
- Operating system and architecture
- Steps to reproduce
- Expected vs actual behavior
- Relevant logs (redact any secrets)

### Feature Requests

Describe the problem you're trying to solve, not just the solution you want. This helps us find the best approach.

## Code Standards

### Go
- `go fmt` and `go vet` must pass with no errors
- Follow standard Go conventions — [Effective Go](https://go.dev/doc/effective-go)
- All exported functions need doc comments
- Error handling: never ignore errors

### Frontend (TypeScript/React)
- TypeScript strict mode — no `any` types
- ESLint must pass with no errors
- Use functional components with hooks
- API calls go through `src/lib/api.ts`, not directly in components

## Releasing

The version pill on Portal and Admin headers, the `depsilo version`
subcommand, and `/api/v1/stats` all derive their value from
`internal/version.Version`, populated at build time via `-ldflags`
from `git describe`.

**Release tags MUST use the `vX.Y.Z` semver form** (e.g. `v0.3.0`,
`v1.2.3-rc1`). Two reasons:

1. The Makefile runs `git describe --tags --match 'v*'` — any tag
   without a leading `v` is invisible to the version pipeline. Pushing
   a `0.3.0` or `release-0.3.0` or `portal-redesign-complete` tag
   silently falls back to the previous semver tag, so the UI keeps
   showing the old version until you re-tag.
2. The frontend `formatVersion()` helper strips the leading `v` from
   semver values and re-prepends it, normalizing the pill to `v0.3.0`.
   Tags with non-numeric leading characters bypass this and render raw.

### Cutting a release

```bash
# 1. Make sure you're on main with a clean tree.
git checkout main && git pull && git status

# 2. Tag with leading 'v'.
git tag v0.3.0
git push origin v0.3.0

# 3. Build — VERSION is auto-derived from the tag.
make build

# 4. Verify.
./bin/depsilo version
# Depsilo 0.3.0 (commit: <hash>, built: <iso-date>)
```

Between releases, `git describe` outputs something like
`v0.3.0-12-gabc1234-dirty` — the version pill displays this as
`v0.3.0+dev` (hover for the full string). That is the intended
"in-progress build" indicator.

## Code of Conduct

This project follows the [Contributor Covenant Code of Conduct](https://www.contributor-covenant.org/version/2/1/code_of_conduct/). By participating, you are expected to uphold this code.
