package contract

// HealthResponse answers GET /health and GET /live; a live process is always
// healthy. Uptime is a Go duration string such as "1h2m3.5s".
type HealthResponse struct {
	Status  string `json:"status" tstype:"'healthy'"`
	Version string `json:"version"`
	Uptime  string `json:"uptime"`
}

// PublicPackageSummary is one cached package aggregated over its files.
// LastAccessed is the database's timestamp text for the newest access.
type PublicPackageSummary struct {
	PackageName  string `json:"package_name"`
	AdapterType  string `json:"adapter_type"`
	VersionCount int64  `json:"version_count"`
	TotalSize    int64  `json:"total_size"`
	TotalHits    int64  `json:"total_hits"`
	LastAccessed string `json:"last_accessed"`
}

// PublicPackageListResponse answers GET /api/v1/packages.
type PublicPackageListResponse struct {
	Total      int64                  `json:"total"`
	Page       int                    `json:"page"`
	PerPage    int                    `json:"per_page"`
	TotalPages int64                  `json:"total_pages"`
	Items      []PublicPackageSummary `json:"items"`
}

// PublicPackageFile is one cached file of a package. Times are RFC 3339.
type PublicPackageFile struct {
	FileName     string `json:"file_name"`
	Size         int64  `json:"size"`
	HitCount     int64  `json:"hit_count"`
	CachedAt     string `json:"cached_at"`
	LastAccessed string `json:"last_accessed"`
	ContentType  string `json:"content_type"`
}

// PublicPackageDetail answers GET /api/v1/packages/:type/:name.
type PublicPackageDetail struct {
	Name        string              `json:"name"`
	AdapterType string              `json:"adapter_type"`
	Versions    []PublicPackageFile `json:"versions"`
	TotalHits   int64               `json:"total_hits"`
	TotalSize   int64               `json:"total_size"`
}

// DiscoverEcosystem is one ecosystem this instance serves, with its route.
type DiscoverEcosystem struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	Purpose string `json:"purpose"`
}

// DiscoverEndpoints lists service paths by stable key. PublicPackages keeps
// its legacy key although the route now requires read authentication.
type DiscoverEndpoints struct {
	AdminDashboard    string `json:"admin_dashboard"`
	AdminLicense      string `json:"admin_license"`
	AdminLogin        string `json:"admin_login"`
	AdminTrial        string `json:"admin_trial"`
	AgentPrompt       string `json:"agent_prompt"`
	Discover          string `json:"discover"`
	EventsStream      string `json:"events_stream"`
	Health            string `json:"health"`
	IntegrationPrompt string `json:"integration_prompt"`
	MCP               string `json:"mcp"`
	Metrics           string `json:"metrics"`
	PublicPackages    string `json:"public_packages"`
	PublicStats       string `json:"public_stats"`
	SetupStatus       string `json:"setup_status"`
}

// DiscoverEndpointAuthentication describes the credentials an authenticated
// endpoint in DiscoverEndpoints expects.
type DiscoverEndpointAuthentication struct {
	EventsStream   string `json:"events_stream"`
	MCP            string `json:"mcp"`
	PublicPackages string `json:"public_packages"`
}

// DiscoverMCP describes the Model Context Protocol endpoint.
type DiscoverMCP struct {
	Authentication string `json:"authentication"`
	Protocol       string `json:"protocol"`
	Summary        string `json:"summary"`
	Transport      string `json:"transport"`
	URL            string `json:"url"`
}

// DiscoverAgentSetup points coding agents at their setup prompts.
// ReadmeAnchor keeps its historical key; it links the deployment guide.
type DiscoverAgentSetup struct {
	LocalBootstrapPromptURL     string `json:"local_bootstrap_prompt_url"`
	PortalURL                   string `json:"portal_url"`
	ProjectIntegrationPromptURL string `json:"project_integration_prompt_url"`
	PromptURL                   string `json:"prompt_url"`
	ReadmeAnchor                string `json:"readme_anchor"`
	Summary                     string `json:"summary"`
}

// DiscoverResponse answers GET /api/v1/discover and the MCP discover
// resource: a self-description for agents and automation. Keys are in
// alphabetical order, as they were when this body was a map.
type DiscoverResponse struct {
	AgentSetup             DiscoverAgentSetup             `json:"agent_setup"`
	Commit                 string                         `json:"commit"`
	Description            string                         `json:"description"`
	Ecosystems             []DiscoverEcosystem            `json:"ecosystems"`
	EndpointAuthentication DiscoverEndpointAuthentication `json:"endpoint_authentication"`
	Endpoints              DiscoverEndpoints              `json:"endpoints"`
	Homepage               string                         `json:"homepage"`
	MCP                    DiscoverMCP                    `json:"mcp"`
	Repository             string                         `json:"repository"`
	Service                string                         `json:"service"`
	SetupRequired          bool                           `json:"setup_required"`
	Version                string                         `json:"version"`
}
