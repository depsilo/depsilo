package admin

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"depsilo/internal/db"
	"depsilo/internal/middleware"
	"depsilo/internal/sbom"
)

type ProjectsHandler struct {
	db *gorm.DB
}

func NewProjectsHandler(database *gorm.DB) *ProjectsHandler {
	return &ProjectsHandler{db: database}
}

var slugRegex = regexp.MustCompile(`[^a-z0-9-]+`)

func toSlug(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	s = strings.ReplaceAll(s, " ", "-")
	s = slugRegex.ReplaceAllString(s, "")
	s = strings.Trim(s, "-")
	if s == "" {
		s = "project"
	}
	return s
}

func generateToken() string {
	b := make([]byte, 24)
	rand.Read(b)
	return "depsilo_proj_" + hex.EncodeToString(b)
}

func writeProjectLookupError(c *gin.Context, err error) {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "project not found"})
		return
	}
	c.JSON(http.StatusInternalServerError, gin.H{"code": "DB_ERROR", "message": err.Error()})
}

func writeProjectDBError(c *gin.Context, err error) {
	c.JSON(http.StatusInternalServerError, gin.H{"code": "DB_ERROR", "message": err.Error()})
}

// List returns all projects.
func (h *ProjectsHandler) List(c *gin.Context) {
	var projects []db.Project
	if err := h.db.Order("datetime(created_at) DESC").Find(&projects).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "DB_ERROR", "message": err.Error()})
		return
	}

	items := make([]projectSummaryResponse, len(projects))
	for i, project := range projects {
		var count int64
		if err := h.db.Model(&db.ProjectPackage{}).Where("project_id = ?", project.ID).Count(&count).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": "DB_ERROR", "message": err.Error()})
			return
		}
		var lastActivity *time.Time
		var latest db.ProjectPackage
		if err := h.db.Where("project_id = ?", project.ID).Order("last_seen_at DESC").First(&latest).Error; err == nil {
			lastActivity = &latest.LastSeenAt
		} else if err != gorm.ErrRecordNotFound {
			c.JSON(http.StatusInternalServerError, gin.H{"code": "DB_ERROR", "message": err.Error()})
			return
		}
		items[i] = projectSummaryResponse{
			ID: project.ID, Name: project.Name, Slug: project.Slug, Description: project.Description,
			PackageCount: count, LastActivityAt: lastActivity, CreatedAt: project.CreatedAt, UpdatedAt: project.UpdatedAt,
		}
	}

	c.JSON(http.StatusOK, projectListResponse{Items: items, Total: len(items)})
}

// Create creates a new project and returns its token (shown once).
func (h *ProjectsHandler) Create(c *gin.Context) {
	var body createProjectRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_BODY", "message": err.Error()})
		return
	}

	slug := toSlug(body.Name)

	// Check slug uniqueness
	var existing db.Project
	if err := h.db.Where("slug = ?", slug).First(&existing).Error; err == nil {
		c.JSON(http.StatusConflict, gin.H{"code": "SLUG_EXISTS", "message": "a project with this name already exists"})
		return
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		writeProjectDBError(c, err)
		return
	}

	token := generateToken()
	tokenHash := middleware.HashProjectToken(token)

	project := db.Project{
		Name:        body.Name,
		Slug:        slug,
		Description: body.Description,
		TokenHash:   tokenHash,
		CreatedBy:   "admin",
	}
	if err := h.db.Create(&project).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "CREATE_FAILED", "message": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, createProjectResponse{
		ID: project.ID, Name: project.Name, Slug: project.Slug, Description: project.Description,
		Token: token, ProxyURL: projectProxyURL(c.Request, project.Slug), CreatedAt: project.CreatedAt,
	})
}

// Detail returns project info with package statistics.
func (h *ProjectsHandler) Detail(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))

	var project db.Project
	if err := h.db.First(&project, id).Error; err != nil {
		writeProjectLookupError(c, err)
		return
	}

	var totalPackages int64
	if err := h.db.Model(&db.ProjectPackage{}).Where("project_id = ?", project.ID).Count(&totalPackages).Error; err != nil {
		writeProjectDBError(c, err)
		return
	}

	type ecoCount struct {
		Ecosystem string `json:"ecosystem"`
		Count     int64  `json:"count"`
	}
	var breakdown []ecoCount
	if err := h.db.Model(&db.ProjectPackage{}).
		Select("ecosystem, count(*) as count").
		Where("project_id = ?", project.ID).
		Group("ecosystem").
		Find(&breakdown).Error; err != nil {
		writeProjectDBError(c, err)
		return
	}

	ecoMap := make(map[string]int64)
	for _, e := range breakdown {
		ecoMap[e.Ecosystem] = e.Count
	}

	var lastActivity *time.Time
	var lastPkg db.ProjectPackage
	if err := h.db.Where("project_id = ?", project.ID).Order("last_seen_at DESC").First(&lastPkg).Error; err == nil {
		lastActivity = &lastPkg.LastSeenAt
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		writeProjectDBError(c, err)
		return
	}

	c.JSON(http.StatusOK, projectDetailResponse{
		ID: project.ID, Name: project.Name, Slug: project.Slug, Description: project.Description,
		ProxyURL: projectProxyURL(c.Request, project.Slug), PackageCount: totalPackages,
		EcosystemBreakdown: ecoMap, LastActivityAt: lastActivity,
		CreatedAt: project.CreatedAt, UpdatedAt: project.UpdatedAt,
	})
}

// Update updates project name and description.
func (h *ProjectsHandler) Update(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))

	var project db.Project
	if err := h.db.First(&project, id).Error; err != nil {
		writeProjectLookupError(c, err)
		return
	}

	var body updateProjectRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_BODY", "message": err.Error()})
		return
	}

	updates := map[string]interface{}{}
	if body.Name != nil {
		updates["name"] = *body.Name
		updates["slug"] = toSlug(*body.Name)
	}
	if body.Description != nil {
		updates["description"] = *body.Description
	}

	if err := h.db.Model(&project).Updates(updates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "DB_ERROR", "message": err.Error()})
		return
	}
	if err := h.db.First(&project, project.ID).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "DB_ERROR", "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"id": project.ID, "name": project.Name, "slug": project.Slug, "description": project.Description,
		"created_at": project.CreatedAt, "updated_at": project.UpdatedAt,
	})
}

// Delete removes a project and all its package records.
func (h *ProjectsHandler) Delete(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))

	var project db.Project
	if err := h.db.First(&project, id).Error; err != nil {
		writeProjectLookupError(c, err)
		return
	}

	if err := h.db.Transaction(func(tx *gorm.DB) error {
		tx = tx.Session(&gorm.Session{SkipDefaultTransaction: true})
		if err := tx.Where("project_id = ?", project.ID).Delete(&db.ProjectPackage{}).Error; err != nil {
			return err
		}
		return tx.Delete(&project).Error
	}); err != nil {
		writeProjectDBError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "deleted"})
}

// ListPackages returns packages recorded for a project.
func (h *ProjectsHandler) ListPackages(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var project db.Project
	if err := h.db.First(&project, id).Error; err != nil {
		writeProjectLookupError(c, err)
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	perPage, _ := strconv.Atoi(c.DefaultQuery("per_page", "50"))
	if page < 1 {
		page = 1
	}
	if perPage < 1 || perPage > 100 {
		perPage = 50
	}

	query := h.db.Model(&db.ProjectPackage{}).Where("project_id = ?", project.ID)

	if eco := c.Query("ecosystem"); eco != "" {
		query = query.Where("ecosystem = ?", eco)
	}
	if search := c.Query("search"); search != "" {
		query = query.Where("package_name LIKE ?", "%"+search+"%")
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		writeProjectDBError(c, err)
		return
	}

	var packages []db.ProjectPackage
	if err := query.Order("last_seen_at DESC").
		Offset((page - 1) * perPage).
		Limit(perPage).
		Find(&packages).Error; err != nil {
		writeProjectDBError(c, err)
		return
	}

	c.JSON(http.StatusOK, projectPackagesResponse{Items: toProjectPackageResponses(packages), Total: total, Page: page})
}

// ExportSBOM generates and downloads an SBOM for a project.
func (h *ProjectsHandler) ExportSBOM(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))

	var project db.Project
	if err := h.db.First(&project, id).Error; err != nil {
		writeProjectLookupError(c, err)
		return
	}

	// Get packages
	query := h.db.Where("project_id = ?", project.ID)
	if eco := c.Query("ecosystem"); eco != "" {
		query = query.Where("ecosystem = ?", eco)
	}

	var packages []db.ProjectPackage
	if err := query.Order("ecosystem, package_name, version").Find(&packages).Error; err != nil {
		writeProjectDBError(c, err)
		return
	}

	format := c.DefaultQuery("format", "spdx")
	gen := sbom.NewGenerator(h.db)

	var data []byte
	var err error
	var filename string

	switch format {
	case "cyclonedx":
		data, err = gen.GenerateCycloneDX(&project, packages)
		filename = fmt.Sprintf("%s-sbom-%s.cyclonedx.json", project.Slug, time.Now().Format("2006-01-02"))
	default:
		data, err = gen.GenerateSPDX(&project, packages)
		filename = fmt.Sprintf("%s-sbom-%s.spdx.json", project.Slug, time.Now().Format("2006-01-02"))
	}

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "GENERATE_FAILED", "message": err.Error()})
		return
	}

	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	c.Data(http.StatusOK, "application/json", data)
}

// RegenerateToken creates a new token for a project.
func (h *ProjectsHandler) RegenerateToken(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))

	var project db.Project
	if err := h.db.First(&project, id).Error; err != nil {
		writeProjectLookupError(c, err)
		return
	}

	token := generateToken()
	tokenHash := middleware.HashProjectToken(token)
	if err := h.db.Model(&project).Update("token_hash", tokenHash).Error; err != nil {
		writeProjectDBError(c, err)
		return
	}

	c.JSON(http.StatusOK, regenerateProjectTokenResponse{Token: token, ProxyURL: projectProxyURL(c.Request, project.Slug)})
}
