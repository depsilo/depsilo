# Contributing to Depsilo

Thanks for helping improve Depsilo. This guide covers the contribution
workflow; the short Codex and repository map lives in [AGENTS.md](AGENTS.md).

## Set up

Required toolchains are pinned by `go.mod` and `web/package.json`.

```bash
make setup
make dev
make logs
```

A checked-in `config.toml` is not required for the normal first-run flow.
For browser tests, install Chromium once with `make setup-ui`.

See [the quick-start guide](docs/development/quick-start.md) for foreground
operation, hot reload, local state, and cleanup behavior.

## Make a change

1. Locate the owning module with the
   [change map](docs/development/change-map.md).
2. Preserve unrelated worktree changes.
3. Add or update the smallest test that proves the behavior.
4. Run focused checks while iterating.
5. Run `make check` before opening a normal pull request.

Keep these project invariants in mind:

- Stream large artifacts instead of buffering whole responses.
- Preserve signed repository metadata bytes.
- Keep machine/package routes out of the SPA fallback.
- Do not expose secrets, credentials, or private upstream URLs.
- Update both Chinese and English strings for user-visible copy.
- Treat SQLite as the current single-instance authority.

Architecture and protocol boundaries are described in
[docs/development/architecture.md](docs/development/architecture.md).

## Verify

```bash
make test          # fast Go suite
make test-ui       # focused mocked browser smoke
make check         # normal local gate
make verify        # complete offline gate
```

`make security` and `make test-e2e` require network access and are separate
from the offline gate. Adapter changes should also run the relevant real-client
test. Choose checks with [the testing guide](docs/development/testing.md).

## Pull requests

Keep each pull request focused and explain:

- the user-visible or operational problem;
- the chosen design and important trade-offs;
- the exact checks run;
- any migration, compatibility, or rollback concern.

Update current documentation when behavior changes. Do not add implementation
plans to the permanent documentation tree; use the issue or pull request for
temporary planning, and record accepted architectural decisions as ADRs.

## Security

Do not report vulnerabilities in a public issue. Follow
[SECURITY.md](SECURITY.md).

## Releases

Maintainers should follow [RELEASE_CHECKLIST.md](RELEASE_CHECKLIST.md) and
[docs/release-verification.md](docs/release-verification.md).
