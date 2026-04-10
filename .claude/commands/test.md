---
name: test
description: Run all tests and report results
---

Run the following steps:
1. `make test` — runs `go test ./...`
2. If any tests fail, read the failing test file and the code under test
3. Diagnose the root cause and suggest a fix
4. Do NOT auto-fix without asking — just report what failed and why
