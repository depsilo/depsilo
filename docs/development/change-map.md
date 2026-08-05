# Change map

Start at the owning module, then follow its interface and nearby tests. Broad
repository searches should confirm a path, not substitute for finding the
owner.

| Change | Primary files | Closest tests | Extra reading |
| --- | --- | --- | --- |
| CLI command or flag | `cmd/depsilo/`, `internal/cli/` | `internal/cli/*_test.go` | [Quick start](quick-start.md) |
| Startup or shutdown | `internal/server/` | `internal/server/*_test.go` | [Architecture](architecture.md) |
| Config field or precedence | `internal/config/`, `config.example.toml` | `internal/config/*_test.go` | Admin authority guide |
| Package protocol | `internal/adapter/<ecosystem>/` | adjacent adapter tests | relevant dated spec only if needed |
| Shared cache behavior | `internal/cache/` | adjacent cache tests | cache interfaces in source |
| Upstream selection/health | `internal/upstream/` | adjacent upstream tests | ADR-0001, Admin authority guide |
| Public/Admin HTTP contract | `internal/api/` | adjacent contract/handler tests | Admin authority guide |
| Database model/migration | `internal/db/` | migration and repository tests | relevant ADR |
| Supply-chain rule | `internal/quarantine/`, `internal/blocklist/`, `internal/tamper/`, `internal/rules/`, `internal/security/` | adjacent tests + tagged integration | PRODUCT, relevant ADR/spec |
| Compiler cache | `internal/compilecache/`, `internal/api/ccache.go`, `internal/api/sccache.go` | compile-cache Go tests | compiler-cache guide |
| Portal UI | `web/src/portal/` | `web/e2e/portal-*.spec.ts` | DESIGN.md |
| Admin UI | `web/src/admin/` | closest Admin Playwright spec; pure logic in `web/unit/` | DESIGN.md, Admin authority guide |
| Shared UI primitive/theme | `web/src/components/`, `web/src/index.css` | focused browser/accessibility tests | DESIGN.md |
| Release/build pipeline | `.github/workflows/`, `.goreleaser.yaml`, `Dockerfile`, scripts | `scripts/test-*.sh` | release verification guide |
| Real client behavior | `testground/docker-<ecosystem>/` | `make test-docker-<ecosystem>` | testground README |

## Adding or changing an ecosystem

Do not follow old adapter checklists by rote. Inspect the current catalog and
registration path under `internal/server`, then account for every affected
interface:

1. Protocol handler, cache keys, rewrites, TTL, and Upstream behavior.
2. Root and project-scoped routing where applicable.
3. Config/bootstrap and runtime discovery.
4. Portal configuration data and both locales.
5. Adjacent Go tests, mock-upstream integration, and a real-client fixture.

Passthrough, metadata-rewriting, signed-artifact, Docker OCI, and Hugging Face
adapters have materially different constraints. Choose a reference with the
same protocol shape rather than the shortest implementation.

## Before declaring a change complete

- Confirm the changed behavior has one owning module and one clear interface.
- Remove obsolete tests or docs that the new interface replaces.
- Update current guides only when a stable contract changed; do not rewrite a
  historical spec to pretend it predicted the final implementation.
- Run the smallest focused test, then the matching row in the
  [testing guide](testing.md).
