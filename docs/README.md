# Documentation

Start with the [project overview](../README.md) or [中文介绍](README_zh.md).
Use the guides below for the task at hand.

## Deploy and use

| Task | Guide |
| --- | --- |
| Install, initialize, or upgrade | [Deployment](deployment.md) |
| Find configuration fields and defaults | [Configuration reference](../config.example.toml) |
| Choose supply-chain controls | [Policy overview](supply-chain.md) · [Package rules and ecosystem coverage](package-rules.md) |
| Connect a login provider or directory | [OIDC](oidc.md) · [LDAP / LDAPS](ldap.md) |
| Connect ccache or sccache | [Compiler cache](compile-cache.md) |
| Connect coding agents or MCP clients | [AI coding agents](deployment.md#ai-coding-agents) |
| Share cached downloads between instances | [Cascade](cascade.md) |
| Retain and verify a dependency-set draft | [Preparation catalog](distribution.md) (no offline serving yet) |
| Verify cache reuse and cascade traffic | [Controlled benchmark](../testground/README.md#controlled-cache-and-cascade-evidence) |
| Evaluate an isolated installation | [Trial guide](trial-guide.md) |

## Operate and secure

| Task | Guide |
| --- | --- |
| Check service health and diagnose failures | [Diagnostics](deployment.md#health-checks) · [Self-test checklist](self-test-checklist.md) |
| Understand Admin APIs and configuration ownership | [Admin control plane](admin-control-plane.md) |
| Forward audit events and verify the audit chain | [SIEM audit routing](siem-audit-routing.md) |
| Verify signed releases, images, and SBOMs | [Release verification](release-verification.md) |
| Check compatibility and migration promises | [Compatibility policy](compatibility.md) |
| Review security boundaries or report a vulnerability | [Threat model](threat-model.md) · [Security policy](../SECURITY.md) |
| See release changes | [Changelog](../CHANGELOG.md) |

## Develop and contribute

| Task | Guide |
| --- | --- |
| Install tools and run a local service | [Development quick start](development/quick-start.md) |
| Understand runtime ownership | [Architecture](development/architecture.md) |
| Find the files for a change | [Change map](development/change-map.md) |
| Choose the right checks | [Testing](development/testing.md) |
| Implement planned Python offline delivery | [First milestone specification](specs/2026-10-26-python-offline-distribution.md) · [Acceptance fixture](../testground/offline-pypi/README.md) |
| Work on shared UI components | [Frontend component ownership](development/frontend-components.md) |
| Contribute a change | [Contributing](../CONTRIBUTING.md) |

Coding agents should begin at [AGENTS.md](../AGENTS.md). Product scope lives in
[PRODUCT.md](../PRODUCT.md); shared domain vocabulary lives in
[CONTEXT.md](../CONTEXT.md).

## Decisions and historical evidence

- [Architecture decisions](adr/) record accepted choices. Read the decisions
  relevant to the module being changed.
- [Cache and distribution direction](adr/0008-dependency-cache-and-distribution.md)
  records the current priorities: bandwidth reuse, cascade, and planned offline
  dependency delivery, with current runtime and entitlement boundaries preserved.
- [Specs](specs/) and [research](research/) are dated evidence, not current
  implementation instructions. The running code and current guides may have
  evolved since they were written.
- [DIRECTION.md](DIRECTION.md) is a historical strategy snapshot;
  [PRODUCT.md](../PRODUCT.md) is the current product authority.

Completed task plans stay in Git history rather than the default documentation
path, so new contributors can find current guidance first.
