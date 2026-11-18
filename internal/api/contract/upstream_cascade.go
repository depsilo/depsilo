package contract

// AdminCascadePeer is one parent Depsilo this instance may relay through.
// Peer tokens are never returned.
type AdminCascadePeer struct {
	Name               string `json:"name"`
	URL                string `json:"url"`
	ForwardCredentials bool   `json:"forward_credentials"`
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
