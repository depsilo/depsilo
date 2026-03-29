# Contributing to Depslio

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
git clone https://github.com/your-org/depslio.git
cd depslio

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
2. **Create a branch** from `main`: `git checkout -b feat/my-feature`
3. **Make your changes** and commit using [Conventional Commits](https://www.conventionalcommits.org/):
   ```
   feat: add support for conda repositories
   fix: handle upstream timeout gracefully
   docs: update Quick Start instructions
   refactor: extract cache key generation
   ```
4. **Push** to your fork: `git push origin feat/my-feature`
5. **Open a Pull Request** against `main`

### PR Guidelines

- Keep PRs focused — one feature or fix per PR
- Update documentation if behavior changes
- Add tests for new functionality
- Fill out the PR template completely

## Reporting Issues

### Bug Reports

Please include:
- Depslio version (`depslio --version` or Docker image tag)
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

## Code of Conduct

This project follows the [Contributor Covenant Code of Conduct](https://www.contributor-covenant.org/version/2/1/code_of_conduct/). By participating, you are expected to uphold this code.
