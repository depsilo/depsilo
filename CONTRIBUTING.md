# Contributing to Depsilo

Contributions are welcome! Whether it's a bug fix, new feature, or documentation improvement — we appreciate your help.

## Development Environment

### Prerequisites

- Go 1.25.6+
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
# Build web/dist before starting the embedded backend
cd web && npm run build && cd ..

# Terminal 1 — Backend
go run ./cmd/depsilo serve

# Terminal 2 — Frontend (hot reload)
cd web && npm run dev
```

The frontend dev server runs on `http://localhost:5173` and proxies API requests to the backend on `:23333`.

## Testing

Depsilo has three test tiers, each runnable independently:

### Unit tests (fast, no network)

```bash
make test-unit              # go test ./tests/unit/...
go test ./... -race         # all in-package and top-level tests
```

Cover URL rewriting, cache keys, streaming/hash invariants, quarantine, blocklist,
tamper detection, access-log rollups, security, licensing, and package-name extraction.

### Integration tests (mock upstream, no network)

```bash
make test-integration       # boots a mock upstream + a local Depsilo process
```

Covers 13 package ecosystems plus Docker Registry behavior against `tests/mock/`,
including blocklist and tamper paths. It does not yet exercise the minimum-age
gate over HTTP. The suite is offline and is the main HTTP-level regression net.

### End-to-end tests (real clients, real upstreams, Docker)

Each ecosystem has its own tiny `testground/docker-<eco>/Dockerfile` whose `RUN` steps invoke the real client (pip / npm / mvn / dotnet / R / helm / cargo / ...) against a running Depsilo, talking through the host's `docker0` gateway.

```bash
make test-docker-pypi       # pip install requests through Depsilo
make test-docker-npm        # npm install lodash
make test-docker-go         # go get golang.org/x/text
make test-docker-cargo      # cargo fetch serde
make test-docker-maven      # mvn dependency:get guava
make test-docker-rubygems   # gem install rake
make test-docker-composer   # composer require monolog/monolog
make test-docker-nuget      # dotnet add Newtonsoft.Json
make test-docker-conda      # conda install requests
make test-docker-cran       # R install.packages('jsonlite')
make test-docker-helm       # helm repo add + index.yaml fetch
make test-docker-apt        # apt install curl/wget/jq
make test-docker-huggingface # huggingface_hub download through Depsilo

make test-docker-all        # 13 defined non-Docker targets; Alpine is not covered
make test-e2e               # alias for test-docker-all
```

The Docker Registry adapter has its own opt-in target because verifying `docker pull` needs Docker-in-Docker (`--privileged`), which isn't appropriate as a default for everyone's machine:

```bash
make test-docker-docker     # docker pull alpine via dind through Depsilo
```

Each target depends on `make dev` (starts a backgrounded Depsilo). When you're done:

```bash
make stop                   # kill the background Depsilo
make test-clean             # remove all depsilo-test-* images
```

Adding a new ecosystem? One new `testground/docker-<eco>/Dockerfile` plus one `test-docker-<eco>:` target in the Makefile — that's the entire surface.

## Before pushing

Run the same gates as CI before `git push`:

```bash
make lint
go test ./... -race
make test-integration
cd web && npm run type-check && npm run build && cd ..
go build -o bin/depsilo ./cmd/depsilo
```

`make lint-i18n` alone catches the most common contributor mistake: adding a `t('...')` call without updating both `web/src/i18n/zh.ts` and `web/src/i18n/en.ts`, or letting a `{{var}}` placeholder drift between the two locales. Failing this check means the UI will render raw `ns.key` strings or have a missing variable in one language — both regressions that are easy to miss in manual review.

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
- Depsilo version (`depsilo version` or Docker image tag)
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
- TypeScript strict mode; do not add new explicit `any` types
- `npm run type-check` and `npm run build` must pass. The repository has a known
  ESLint baseline; fix errors in touched code and do not increase the count.
- Use functional components with hooks
- API calls go through `src/lib/api.ts`, not directly in components

## Releasing

The version pill on Portal and Admin headers, the `depsilo version`
subcommand, and `/api/v1/stats` all derive their value from
`internal/version.Version`, populated at build time via `-ldflags`
from `git describe`.

**Release tags MUST use the `vX.Y.Z` semver form** (e.g. `v0.8.0`,
`v1.2.3-rc1`). Two reasons:

1. The Makefile runs `git describe --tags --match 'v*'` — any tag
   without a leading `v` is invisible to the version pipeline. Pushing
   a `0.8.0` or `release-0.8.0` or `portal-redesign-complete` tag
   silently falls back to the previous semver tag, so the UI keeps
   showing the old version until you re-tag.
2. The frontend `formatVersion()` helper strips the leading `v` from
   semver values and re-prepends it, normalizing the pill to `v0.8.0`.
   Tags with non-numeric leading characters bypass this and render raw.

### Cutting a release

```bash
# 1. Make sure you're on master with a clean tree.
git checkout master && git pull --ff-only && git status

# 2. Tag with leading 'v'.
VERSION=vX.Y.Z
git tag -a "$VERSION" -m "$VERSION"
git push origin "$VERSION"

# 3. Build — VERSION is auto-derived from the tag.
make build

# 4. Verify.
./bin/depsilo version
# Depsilo X.Y.Z (commit: <hash>, built: <iso-date>)
```

Between releases, `git describe` outputs something like
`v0.8.0-12-gabc1234-dirty` — the version pill displays this as
`v0.8.0+dev` (hover for the full string). That is the intended
"in-progress build" indicator.

## Code of Conduct

This project follows the [Contributor Covenant Code of Conduct](https://www.contributor-covenant.org/version/2/1/code_of_conduct/). By participating, you are expected to uphold this code.
