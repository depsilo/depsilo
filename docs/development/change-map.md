# Change map

Start at the owning module, then follow its interface and nearby tests. Broad
repository searches should confirm a path, not substitute for finding the
owner.

| Change | Primary files | Closest tests | Extra reading |
| --- | --- | --- | --- |
| CLI command or flag | `internal/cli/`; a command is registered once, in the `commands` table of `internal/cli/root.go` | `internal/cli/*_test.go` | [CLI conventions](quick-start.md#cli-conventions) |
| Startup or shutdown | `internal/server/` | `internal/server/*_test.go` | [Architecture](architecture.md) |
| Config field or precedence | `internal/config/`, `config.example.toml` | `internal/config/*_test.go` | Admin authority guide |
| Package protocol | `internal/adapter/<ecosystem>/` | adjacent adapter tests | relevant dated spec only if needed |
| Shared cache behavior | `internal/cache/` | adjacent cache tests | cache interfaces in source |
| Retained dependency-set preparation | `internal/distribution/`, `internal/api/admin/distribution.go`, `internal/cli/distribution.go`, `internal/cli/distribution_transfer.go` | distribution Store/storage/transfer, API, and CLI tests; `make test-s3` for S3 | [Preparation catalog and transfer bundles](../distribution.md) |
| Upstream selection/health | `internal/upstream/` | adjacent upstream tests | ADR-0001, Admin authority guide |
| Upstream egress guard, default proxy, declared private ranges, check failure reasons | `internal/egress/`, `internal/upstream/request.go`, `internal/upstream/failure.go`, `internal/config/egress.go`; Docker registries: `internal/adapter/docker/egress.go` | `internal/egress/*_test.go`, `internal/upstream/egress_failure_test.go`, `internal/upstream/request_test.go`, `internal/adapter/docker/egress_test.go`, `internal/server/docker_egress_test.go` | [Upstream network egress](../deployment.md#upstream-network-egress) |
| Public/Admin HTTP contract | `internal/api/`; Admin response types are mirrored by hand in `web/src/lib/adminApi.types.ts` | adjacent contract/handler tests | Admin authority guide |
| First-run setup and operator credentials | `internal/api/setup.go`, `internal/api/setup_validation.go`, `internal/api/auth.go`, `internal/api/admin/user.go`, `internal/credential/`, `internal/config/bootstrap_runtime.go`, `internal/config/setup_backup.go`, `internal/cli/setup.go`, `internal/cli/resetpassword.go`, `web/src/setup/`, `web/src/lib/passwordPolicy.ts` | `internal/api/setup_test.go`, `internal/api/auth_test.go`, `internal/credential/*_test.go`, `internal/cli/setup_test.go`, `internal/cli/resetpassword_test.go`, `web/unit/passwordPolicy.test.ts`, `web/e2e/auth-setup-state.spec.ts` | The password rules exist twice: `internal/credential/policy.go` and `web/src/lib/passwordPolicy.ts`. Change both and add a case to the shared vectors in `internal/credential/testdata/password_vectors.json`, which both suites run. [First-run setup](../deployment.md#first-run-setup) |
| Sign-in identity (OIDC, LDAP) | `internal/identity/`, `internal/api/oidc*.go`, `internal/api/ldap.go`, `internal/api/admin/authentication*.go`, `internal/config/oidc*.go`, `internal/config/ldap*.go`, `internal/config/authentication_*.go`, `web/src/admin/pages/Authentication.tsx`, `web/src/lib/identityAuth.ts` | adjacent Go tests; `web/e2e/oidc-account.spec.ts`, `web/e2e/ldap-authentication.spec.ts`, `web/e2e/system-authentication.spec.ts`; provider fixtures in `testground/oidc-casdoor/` and `testground/ldap/` | ADR-0007, [OIDC](../oidc.md), [LDAP](../ldap.md) |
| Backup and restore | `internal/backup/`, `internal/cli/backup.go` | `internal/backup/*_test.go`, `internal/cli/backup_test.go` | [Database and backups](../compatibility.md#database-and-backups) |
| Entitlement, license, and trial | `internal/entitlement/`, `internal/license/`, `internal/trial/`, `internal/api/admin/license.go`, `internal/distribution/quota.go`, `web/src/admin/pages/License.tsx` | adjacent Go tests, `internal/api/admin/license_lifecycle_test.go`, `web/e2e/admin-license.spec.ts` | ADR-0009 |
| Audit and access logs | `internal/audit/`, `internal/accesslog/`, `internal/api/admin/audit*.go`, `internal/api/admin/logs.go`, `internal/cli/audit.go`, `web/src/admin/pages/AuditLogs.tsx`, `web/src/admin/pages/AccessLogs.tsx` | adjacent Go tests, `internal/cli/audit_test.go`, `web/e2e/admin-audit-integrity.spec.ts`, `web/e2e/admin-siem-exporters.spec.ts`, `web/e2e/admin-log-workspace-state.spec.ts` | ADR-0002, [SIEM audit routing](../siem-audit-routing.md) |
| Agent-facing surfaces (discover, prompts, MCP, `init-agent`) | `internal/api/public/discover.go`, `internal/api/public/integration_prompt.go`, `internal/api/public/mcp.go`, `internal/prompts/`, `internal/cli/initagent.go`, `internal/cli/prompt.go`, `web/src/portal/integrationPrompt.ts`, `web/src/portal/components/AIIntegration.tsx` | `internal/api/public/*_test.go`, `internal/prompts/*_test.go`, `internal/api/mcp_ssrf_test.go`, `internal/cli/initagent_test.go`, `internal/cli/prompt_test.go`, `web/unit/integrationPromptScopes.test.ts` | [AI coding agents](../deployment.md#ai-coding-agents) |
| Database model/migration | `internal/db/` | migration and repository tests | relevant ADR |
| Supply-chain rule | `internal/quarantine/`, `internal/blocklist/`, `internal/tamper/`, `internal/rules/`, `internal/security/` | adjacent tests + tagged integration | PRODUCT, relevant ADR/spec |
| Compiler cache | `internal/compilecache/`, `internal/api/ccache.go`, `internal/api/sccache.go` | compile-cache Go tests | compiler-cache guide |
| Portal UI | `web/src/portal/` | `web/e2e/portal-*.spec.ts` | [web/AGENTS.md](../../web/AGENTS.md) |
| Admin UI | `web/src/admin/` | closest Admin Playwright spec; pure logic in `web/unit/` | [web/AGENTS.md](../../web/AGENTS.md), Admin authority guide |
| Shared UI primitive/theme | `web/src/components/ui/`, `web/src/components/`, `web/src/index.css` | focused browser/accessibility tests | [web/AGENTS.md](../../web/AGENTS.md), [component ownership](frontend-components.md) |
| Release/build pipeline | `.github/workflows/`, `.goreleaser.yaml`, `Dockerfile`, scripts | `scripts/test-*.sh` | release verification guide |
| Real client behavior | `testground/docker-<ecosystem>/` | `make test-docker-<ecosystem>` | testground README |

## Adding or changing an ecosystem

Start from the catalog entry in `internal/ecosystem/catalog.go`. Routing, setup
validation, the written configuration, and Upstream bootstrap range over it.
The lists below are kept by hand, so account for every affected interface:

1. Protocol handler, cache keys, rewrites, TTL, and Upstream behavior:
   `internal/adapter/<ecosystem>/`, bound to the catalog entry in
   `internal/server/ecosystem_adapters.go`. Cache kinds are classified in
   `internal/db/cache_kind.go`, package names and package files are read from
   cache keys in `internal/adapter/packagekey/extract.go` and `classify.go`,
   and the default minimum release age is set in
   `internal/quarantine/policy.go`.
2. Root and project-scoped routing where applicable: both follow the catalog
   entry's `Route`. The development proxy keeps its own prefix list in
   `web/vite.config.ts`.
3. Config/bootstrap and runtime discovery: the `Config` field in
   `internal/config/config.go`, the cascade list in
   `internal/config/cascade.go`, built-in default Upstreams in
   `internal/config/loader.go`, the example section in `config.example.toml`,
   and the purpose text in `internal/api/public/discover.go`. An instance that
   already stores Upstreams turns a new ecosystem on only when `config.toml`
   lists `<ecosystem>.upstreams` (`internal/upstream/bootstrap.go`).
4. Portal configuration data and both locales:
   `web/src/lib/ecosystemData.ts`, `web/src/i18n/zh.ts`, and
   `web/src/i18n/en.ts`. The frontend's ecosystem lists are
   `web/src/lib/ecosystemTypes.ts`, `ADMIN_ECOSYSTEMS` in
   `web/src/lib/adminApi.types.ts`, and `web/src/admin/operatorEcosystems.ts`;
   setup defaults are in `web/src/setup/defaults.ts`.
5. Adjacent Go tests, mock-upstream integration, and a real-client fixture:
   `testground/docker-<ecosystem>/Dockerfile`, added to `TEST_DOCKER_ALL_ECOS`
   in the `Makefile` and to the same list in `scripts/test-makefile.sh`.

Add a row for the new ecosystem to `cacheKeySamples` in
`internal/ecosystem/coverage_test.go` and run `go test ./internal/ecosystem`.
It fails and names the list when the cache-kind, package-key, release-age,
discover, or cascade list misses a catalog entry.
`node scripts/test-vite-proxy-routes.mjs` does the same for the Vite prefix
list, and `web/unit/catalogTranslations.test.ts` for the Portal catalog's
translation keys. No test compares the frontend lists in item 4 with the Go
catalog. Other per-ecosystem switches exist (provenance wiring, package rules,
SBOM, agent prompts): search for the name of an ecosystem with the same
protocol shape.

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
