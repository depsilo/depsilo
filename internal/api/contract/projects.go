package contract

import "time"

// Project is a project's identity, as PUT /admin/projects/:id answers it.
type Project struct {
	ID          uint      `json:"id"`
	Name        string    `json:"name"`
	Slug        string    `json:"slug"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// ProjectSummary is one project in the project list. LastActivityAt is null
// until the project's token has fetched a package.
type ProjectSummary struct {
	ID             uint       `json:"id"`
	Name           string     `json:"name"`
	Slug           string     `json:"slug"`
	Description    string     `json:"description"`
	PackageCount   int64      `json:"package_count"`
	LastActivityAt *time.Time `json:"last_activity_at" tstype:"string | null,required"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

// ProjectCapabilities is what the active entitlement allows. ProjectLimit is
// null when the entitlement does not limit the project count.
type ProjectCapabilities struct {
	ProjectLimit  *int `json:"project_limit" tstype:"number | null,required"`
	CanCreate     bool `json:"can_create"`
	CanExportSBOM bool `json:"can_export_sbom"`
}

// ProjectListResponse answers GET /admin/projects.
type ProjectListResponse struct {
	Items        []ProjectSummary    `json:"items"`
	Total        int                 `json:"total"`
	Capabilities ProjectCapabilities `json:"capabilities"`
}

// CreateProjectResponse answers POST /admin/projects. It is the only
// response besides a token regeneration that carries the project token.
type CreateProjectResponse struct {
	ID          uint      `json:"id"`
	Name        string    `json:"name"`
	Slug        string    `json:"slug"`
	Description string    `json:"description"`
	Token       string    `json:"token"`
	ProxyURL    string    `json:"proxy_url"`
	CreatedAt   time.Time `json:"created_at"`
}

// ProjectDetail answers GET /admin/projects/:id. EcosystemBreakdown counts
// recorded packages per ecosystem and is an empty object for a new project.
type ProjectDetail struct {
	ID                 uint             `json:"id"`
	Name               string           `json:"name"`
	Slug               string           `json:"slug"`
	Description        string           `json:"description"`
	ProxyURL           string           `json:"proxy_url"`
	PackageCount       int64            `json:"package_count"`
	EcosystemBreakdown map[string]int64 `json:"ecosystem_breakdown"`
	LastActivityAt     *time.Time       `json:"last_activity_at" tstype:"string | null,required"`
	CreatedAt          time.Time        `json:"created_at"`
	UpdatedAt          time.Time        `json:"updated_at"`
}

// ProjectPackage is one package version a project's token has fetched.
type ProjectPackage struct {
	Ecosystem     string    `json:"ecosystem"`
	PackageName   string    `json:"package_name"`
	Version       string    `json:"version"`
	FirstSeenAt   time.Time `json:"first_seen_at"`
	LastSeenAt    time.Time `json:"last_seen_at"`
	DownloadCount int       `json:"download_count"`
}

// ProjectPackagesResponse answers GET /admin/projects/:id/packages with one
// page of packages.
type ProjectPackagesResponse struct {
	Items []ProjectPackage `json:"items"`
	Total int64            `json:"total"`
	Page  int              `json:"page"`
}

// RegenerateProjectTokenResponse answers POST /admin/projects/:id/token.
type RegenerateProjectTokenResponse struct {
	Token    string `json:"token"`
	ProxyURL string `json:"proxy_url"`
}

// DeleteProjectResponse answers DELETE /admin/projects/:id. Status is always
// "deleted"; a one-value enumeration is a literal tstype because tygo emits
// unions only for two or more constants.
type DeleteProjectResponse struct {
	Status string `json:"status" tstype:"'deleted'"`
}

// ComplianceProfile answers GET /admin/compliance/profile: the non-secret
// state of the CRA facts that project SBOM exports carry.
type ComplianceProfile struct {
	Organization             string `json:"organization"`
	Contact                  string `json:"contact"`
	SigningConfigured        bool   `json:"signing_configured"`
	ComponentAnnotationCount int    `json:"component_annotation_count"`
}
