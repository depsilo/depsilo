package admin

import (
	"crypto/rand"
	"encoding/hex"
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

// List returns all projects.
func (h *ProjectsHandler) List(c *gin.Context) {
	var projects []db.Project
	h.db.Order("created_at DESC").Find(&projects)

	type projectItem struct {
		db.Project
		PackageCount int64 `json:"package_count"`
	}

	items := make([]projectItem, len(projects))
	for i, p := range projects {
		var count int64
		h.db.Model(&db.ProjectPackage{}).Where("project_id = ?", p.ID).Count(&count)
		items[i] = projectItem{Project: p, PackageCount: count}
	}

	c.JSON(http.StatusOK, items)
}

// Create creates a new project and returns its token (shown once).
func (h *ProjectsHandler) Create(c *gin.Context) {
	var body struct {
		Name        string `json:"name" binding:"required"`
		Description string `json:"description"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_BODY", "message": err.Error()})
		return
	}

	slug := toSlug(body.Name)

	// Check slug uniqueness
	var existing db.Project
	if h.db.Where("slug = ?", slug).First(&existing).Error == nil {
		c.JSON(http.StatusConflict, gin.H{"code": "SLUG_EXISTS", "message": "a project with this name already exists"})
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

	// Build proxy URL from request host
	scheme := "http"
	if c.Request.TLS != nil {
		scheme = "https"
	}
	if fwd := c.GetHeader("X-Forwarded-Proto"); fwd != "" {
		scheme = fwd
	}
	proxyURL := scheme + "://" + c.Request.Host + "/p/" + slug

	c.JSON(http.StatusCreated, gin.H{
		"id":          project.ID,
		"name":        project.Name,
		"slug":        project.Slug,
		"description": project.Description,
		"token":       token,
		"proxy_url":   proxyURL,
		"created_at":  project.CreatedAt,
	})
}

// Detail returns project info with package statistics.
func (h *ProjectsHandler) Detail(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))

	var project db.Project
	if err := h.db.First(&project, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "project not found"})
		return
	}

	var totalPackages int64
	h.db.Model(&db.ProjectPackage{}).Where("project_id = ?", project.ID).Count(&totalPackages)

	type ecoCount struct {
		Ecosystem string `json:"ecosystem"`
		Count     int64  `json:"count"`
	}
	var breakdown []ecoCount
	h.db.Model(&db.ProjectPackage{}).
		Select("ecosystem, count(*) as count").
		Where("project_id = ?", project.ID).
		Group("ecosystem").
		Find(&breakdown)

	ecoMap := make(map[string]int64)
	for _, e := range breakdown {
		ecoMap[e.Ecosystem] = e.Count
	}

	var lastActivity *time.Time
	var lastPkg db.ProjectPackage
	if h.db.Where("project_id = ?", project.ID).Order("last_seen_at DESC").First(&lastPkg).Error == nil {
		lastActivity = &lastPkg.LastSeenAt
	}

	// Build proxy URL
	scheme := "http"
	if c.Request.TLS != nil {
		scheme = "https"
	}
	if fwd := c.GetHeader("X-Forwarded-Proto"); fwd != "" {
		scheme = fwd
	}
	proxyURL := scheme + "://" + c.Request.Host + "/p/" + project.Slug

	c.JSON(http.StatusOK, gin.H{
		"id":                  project.ID,
		"name":                project.Name,
		"slug":                project.Slug,
		"description":         project.Description,
		"proxy_url":           proxyURL,
		"package_count":       totalPackages,
		"ecosystem_breakdown": ecoMap,
		"last_activity_at":    lastActivity,
		"created_at":          project.CreatedAt,
	})
}

// Update updates project name and description.
func (h *ProjectsHandler) Update(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))

	var project db.Project
	if err := h.db.First(&project, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "project not found"})
		return
	}

	var body struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_BODY", "message": err.Error()})
		return
	}

	updates := map[string]interface{}{}
	if body.Name != "" {
		updates["name"] = body.Name
		updates["slug"] = toSlug(body.Name)
	}
	if body.Description != "" {
		updates["description"] = body.Description
	}

	h.db.Model(&project).Updates(updates)
	c.JSON(http.StatusOK, project)
}

// Delete removes a project and all its package records.
func (h *ProjectsHandler) Delete(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))

	var project db.Project
	if err := h.db.First(&project, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "project not found"})
		return
	}

	// Delete package records first
	h.db.Where("project_id = ?", project.ID).Delete(&db.ProjectPackage{})
	h.db.Delete(&project)

	c.JSON(http.StatusOK, gin.H{"status": "deleted"})
}

// ListPackages returns packages recorded for a project.
func (h *ProjectsHandler) ListPackages(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	perPage, _ := strconv.Atoi(c.DefaultQuery("per_page", "50"))
	if page < 1 {
		page = 1
	}
	if perPage < 1 || perPage > 100 {
		perPage = 50
	}

	query := h.db.Model(&db.ProjectPackage{}).Where("project_id = ?", id)

	if eco := c.Query("ecosystem"); eco != "" {
		query = query.Where("ecosystem = ?", eco)
	}
	if search := c.Query("search"); search != "" {
		query = query.Where("package_name LIKE ?", "%"+search+"%")
	}

	var total int64
	query.Count(&total)

	var packages []db.ProjectPackage
	query.Order("last_seen_at DESC").
		Offset((page - 1) * perPage).
		Limit(perPage).
		Find(&packages)

	c.JSON(http.StatusOK, gin.H{"items": packages, "total": total, "page": page})
}

// ExportSBOM generates and downloads an SBOM for a project.
func (h *ProjectsHandler) ExportSBOM(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))

	var project db.Project
	if err := h.db.First(&project, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "project not found"})
		return
	}

	// Get packages
	query := h.db.Where("project_id = ?", project.ID)
	if eco := c.Query("ecosystem"); eco != "" {
		query = query.Where("ecosystem = ?", eco)
	}

	var packages []db.ProjectPackage
	query.Order("ecosystem, package_name, version").Find(&packages)

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
		c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "project not found"})
		return
	}

	token := generateToken()
	tokenHash := middleware.HashProjectToken(token)
	h.db.Model(&project).Update("token_hash", tokenHash)

	c.JSON(http.StatusOK, gin.H{"token": token})
}
