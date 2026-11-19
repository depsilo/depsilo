package contract

import "time"

// AdminCascadePeer is one parent Depsilo this instance may relay through.
// Peer tokens are never returned. The remaining fields describe exchanges
// through the parent since the server started or cascade was last applied:
// LastFailure says why the newest one fell back to the origin and is empty
// once one succeeds through the parent again.
type AdminCascadePeer struct {
	Name               string     `json:"name"`
	URL                string     `json:"url"`
	ForwardCredentials bool       `json:"forward_credentials"`
	LastSuccessAt      *time.Time `json:"last_success_at" tstype:"string | null,required"`
	LastFallbackAt     *time.Time `json:"last_fallback_at" tstype:"string | null,required"`
	LastFailure        string     `json:"last_failure"`
	FallbackCount      uint64     `json:"fallback_count"`
}

// AdminCascadeInfo answers GET /admin/cascade: the cascade topology the
// running process uses.
type AdminCascadeInfo struct {
	Enabled           bool               `json:"enabled"`
	InstanceID        string             `json:"instance_id"`
	RelayPath         string             `json:"relay_path"`
	MaxHops           int                `json:"max_hops"`
	MaxTTLSeconds     int64              `json:"max_ttl_seconds"`
	AllowInsecureHTTP bool               `json:"allow_insecure_http"`
	DirectFallback    bool               `json:"direct_fallback"`
	Peers             []AdminCascadePeer `json:"peers"`
}

// AdminCascadeConfigPeer is one configured peer. TokenSet reports an explicit
// per-peer token override; the value is never returned.
type AdminCascadeConfigPeer struct {
	Name               string `json:"name"`
	URL                string `json:"url"`
	TokenSet           bool   `json:"token_set"`
	ForwardCredentials bool   `json:"forward_credentials"`
}

// AdminCascadeConfigState answers GET /admin/cascade/config: the editable
// cascade section of config.toml. TokenSet reports the shared secret without
// returning it; ConfigWritable reports whether config.toml and its directory
// are writable; PendingRestart reports that the running process still uses an
// older version of these values.
type AdminCascadeConfigState struct {
	Enabled           bool                     `json:"enabled"`
	MaxHops           int                      `json:"max_hops"`
	MaxTTL            string                   `json:"max_ttl"`
	AllowInsecureHTTP bool                     `json:"allow_insecure_http"`
	DirectFallback    bool                     `json:"direct_fallback"`
	TokenSet          bool                     `json:"token_set"`
	Peers             []AdminCascadeConfigPeer `json:"peers"`
	ConfigWritable    bool                     `json:"config_writable"`
	PendingRestart    bool                     `json:"pending_restart"`
}

// AdminCascadeConfigUpdateResponse answers PUT /admin/cascade/config.
// Changed lists the changed config keys; it is null when nothing changed.
type AdminCascadeConfigUpdateResponse struct {
	State           AdminCascadeConfigState `json:"state"`
	Changed         []string                `json:"changed" tstype:"string[] | null"`
	RestartRequired bool                    `json:"restart_required"`
}

// AdminCascadePairingResponse answers POST /admin/cascade/pairing with the
// code a child pastes to use this node as its parent. The code carries the
// shared token. EnabledNow reports that the request turned cascade on.
// Loopback warns that URL reaches this node only from this machine.
type AdminCascadePairingResponse struct {
	Code            string `json:"code"`
	URL             string `json:"url"`
	EnabledNow      bool   `json:"enabled_now"`
	Loopback        bool   `json:"loopback"`
	RestartRequired bool   `json:"restart_required"`
}

// AdminCascadeJoinResponse answers POST /admin/cascade/join. Peer is the
// configured parent's name; Bound counts upstreams now routed through it;
// Skipped names upstreams left direct because their origin is local or
// private. InsecureHTTPAllowed reports that joining turned on
// allow_insecure_http for a plain-HTTP parent.
type AdminCascadeJoinResponse struct {
	Peer                string                  `json:"peer"`
	URL                 string                  `json:"url"`
	ParentInstanceID    string                  `json:"parent_instance_id"`
	Bound               int                     `json:"bound"`
	Skipped             []string                `json:"skipped"`
	InsecureHTTPAllowed bool                    `json:"insecure_http_allowed"`
	RestartRequired     bool                    `json:"restart_required"`
	State               AdminCascadeConfigState `json:"state"`
}

// DockerBootstrapState answers GET and POST /admin/docker/bootstrap with the
// Docker Hub activation facts, without registry URLs or credentials.
type DockerBootstrapState struct {
	Configured        bool `json:"configured"`
	Effective         bool `json:"effective"`
	DockerHub         bool `json:"docker_hub"`
	PendingRestart    bool `json:"pending_restart"`
	ConfigWritable    bool `json:"config_writable"`
	BlockedByOverride bool `json:"blocked_by_override"`
}
