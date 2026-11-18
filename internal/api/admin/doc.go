// Package admin implements the authenticated Admin HTTP handlers.
//
// Files are grouped by Admin workspace (overview, cache, upstreams, logs,
// supply chain, projects, system, authentication); a workspace's files depend
// on each other and on the shared files, not on another workspace's files.
// Helpers more than one workspace needs live in the shared files:
//
//   - request.go: query parameters, pagination, strict JSON bodies, the
//     requesting user.
//   - list_filters.go: ecosystem facets and time-window filters for lists.
//   - credentials.go: who may see credential URLs, and their masking.
//   - config_store.go: answering a failed config.toml write.
//
// Response bodies are types in internal/api/contract.
package admin
