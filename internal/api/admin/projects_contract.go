package admin

import (
	"net/http"

	"depsilo/internal/api/contract"
	"depsilo/internal/db"
	"depsilo/internal/requestorigin"
)

type createProjectRequest struct {
	Name        string `json:"name" binding:"required"`
	Description string `json:"description"`
}

type updateProjectRequest struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
}

func projectProxyURL(req *http.Request, slug string) string {
	return requestorigin.Scheme(req) + "://" + req.Host + "/p/" + slug
}

func toProjectPackageResponses(items []db.ProjectPackage) []contract.ProjectPackage {
	responses := make([]contract.ProjectPackage, len(items))
	for i, item := range items {
		responses[i] = contract.ProjectPackage{
			Ecosystem: item.Ecosystem, PackageName: item.PackageName, Version: item.Version,
			FirstSeenAt: item.FirstSeenAt, LastSeenAt: item.LastSeenAt, DownloadCount: item.DownloadCount,
		}
	}
	return responses
}
