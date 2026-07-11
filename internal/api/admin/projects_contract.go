package admin

import (
	"net/http"
	"time"

	"depsilo/internal/db"
)

type createProjectRequest struct {
	Name        string `json:"name" binding:"required"`
	Description string `json:"description"`
}

type updateProjectRequest struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
}

type projectSummaryResponse struct {
	ID             uint       `json:"id"`
	Name           string     `json:"name"`
	Slug           string     `json:"slug"`
	Description    string     `json:"description"`
	PackageCount   int64      `json:"package_count"`
	LastActivityAt *time.Time `json:"last_activity_at"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

type projectListResponse struct {
	Items []projectSummaryResponse `json:"items"`
	Total int                      `json:"total"`
}

type createProjectResponse struct {
	ID          uint      `json:"id"`
	Name        string    `json:"name"`
	Slug        string    `json:"slug"`
	Description string    `json:"description"`
	Token       string    `json:"token"`
	ProxyURL    string    `json:"proxy_url"`
	CreatedAt   time.Time `json:"created_at"`
}

type projectDetailResponse struct {
	ID                 uint             `json:"id"`
	Name               string           `json:"name"`
	Slug               string           `json:"slug"`
	Description        string           `json:"description"`
	ProxyURL           string           `json:"proxy_url"`
	PackageCount       int64            `json:"package_count"`
	EcosystemBreakdown map[string]int64 `json:"ecosystem_breakdown"`
	LastActivityAt     *time.Time       `json:"last_activity_at"`
	CreatedAt          time.Time        `json:"created_at"`
	UpdatedAt          time.Time        `json:"updated_at"`
}

type projectPackageResponse struct {
	Ecosystem     string    `json:"ecosystem"`
	PackageName   string    `json:"package_name"`
	Version       string    `json:"version"`
	FirstSeenAt   time.Time `json:"first_seen_at"`
	LastSeenAt    time.Time `json:"last_seen_at"`
	DownloadCount int       `json:"download_count"`
}

type projectPackagesResponse struct {
	Items []projectPackageResponse `json:"items"`
	Total int64                    `json:"total"`
	Page  int                      `json:"page"`
}

type regenerateProjectTokenResponse struct {
	Token    string `json:"token"`
	ProxyURL string `json:"proxy_url"`
}

func projectProxyURL(req *http.Request, slug string) string {
	scheme := "http"
	if req.TLS != nil {
		scheme = "https"
	}
	if forwarded := req.Header.Get("X-Forwarded-Proto"); forwarded != "" {
		scheme = forwarded
	}
	return scheme + "://" + req.Host + "/p/" + slug
}

func toProjectPackageResponses(items []db.ProjectPackage) []projectPackageResponse {
	responses := make([]projectPackageResponse, len(items))
	for i, item := range items {
		responses[i] = projectPackageResponse{
			Ecosystem: item.Ecosystem, PackageName: item.PackageName, Version: item.Version,
			FirstSeenAt: item.FirstSeenAt, LastSeenAt: item.LastSeenAt, DownloadCount: item.DownloadCount,
		}
	}
	return responses
}
