---
name: lint
description: Run linters on both Go backend and TypeScript frontend
---

Run these checks in parallel:
1. `make lint` (runs golangci-lint + frontend type-check)
2. If golangci-lint is not installed, run `go vet ./...` as fallback

Report all issues found. Group by file. For each issue, show the file, line, and what's wrong.
