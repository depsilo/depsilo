# Depsilo web application

The frontend is a React, TypeScript, and Vite application embedded into the Go
binary for production.

```bash
npm ci
npm run dev
npm run lint
npm run test:unit
npm run test:ui:smoke
```

Run commands from `web/`, or use the root Make targets documented in
[../docs/development/testing.md](../docs/development/testing.md).

The main surfaces are:

- `src/portal/`: anonymous setup guidance and service status;
- `src/admin/`: authenticated operations and policy UI;
- `unit/`: fast Vitest logic and contract tests;
- `e2e/`: Playwright user-flow and visual checks.

Read [AGENTS.md](AGENTS.md) before changing this subtree. UI behavior and
tokens should remain consistent with [../DESIGN.md](../DESIGN.md). Browser
tests use mocked Admin APIs unless a test explicitly starts the Go service.
