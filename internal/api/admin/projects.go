package admin

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"depsilo/internal/config"
	"depsilo/internal/db"
	"depsilo/internal/middleware"
	"depsilo/internal/sbom"
)

type ProjectsHandler struct {
	db         *gorm.DB
	compliance config.ComplianceConfig
}

func NewProjectsHandler(database *gorm.DB) *ProjectsHandler {
	return &ProjectsHandler{db: database}
}

// SetCompliance supplies the operator-declared CRA facts used by SBOM
// exports. It is called once at wiring time.
func (h *ProjectsHandler) SetCompliance(compliance config.ComplianceConfig) {
	h.compliance = compliance
}

// complianceProfile converts the operator config into the generator's lookup.
func (h *ProjectsHandler) complianceProfile() sbom.ComplianceProfile {
	entries := make(map[string]sbom.ComponentFacts, len(h.compliance.Components))
	for _, component := range h.compliance.Components {
		entries[component.ID] = sbom.ComponentFacts{Supplier: component.Supplier, License: component.License}
	}
	return sbom.NewComplianceProfile(h.compliance.Organization, h.compliance.Contact, entries)
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
//
// Query parameters:
//   - format: spdx (default) or cyclonedx
//   - ecosystem: optional adapter identity filter
//   - cra: true adds the NTIA/CRA minimum elements
//   - preset: "technical-file" wraps the SBOM in a CRA technical-file
//     envelope (implies cra) with an operator-facing manifest
//   - sign: true adds a detached Ed25519 signature over the SBOM document
func (h *ProjectsHandler) ExportSBOM(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))

	var project db.Project
	if err := h.db.First(&project, id).Error; err != nil {
		writeProjectLookupError(c, err)
		return
	}

	ecosystem := c.Query("ecosystem")
	format := c.DefaultQuery("format", "spdx")
	if format != "cyclonedx" {
		format = "spdx"
	}
	preset := c.Query("preset")
	if preset != "" && preset != "technical-file" {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    "UNKNOWN_PRESET",
			"message": fmt.Sprintf("unknown SBOM preset %q (want technical-file)", preset),
		})
		return
	}
	cra := preset == "technical-file" || queryFlag(c, "cra")
	sign := queryFlag(c, "sign")

	profile := h.complianceProfile()
	gen := sbom.NewGenerator(h.db)
	components, err := gen.Components(c.Request.Context(), project.ID, ecosystem, profile)
	if err != nil {
		writeProjectDBError(c, err)
		return
	}

	options := sbom.Options{CRA: cra, Profile: profile}
	var document []byte
	var filename string
	switch format {
	case "cyclonedx":
		document, err = gen.GenerateCycloneDX(&project, components, options)
		filename = fmt.Sprintf("%s-sbom-%s.cyclonedx.json", project.Slug, time.Now().Format("2006-01-02"))
	default:
		document, err = gen.GenerateSPDX(&project, components, options)
		filename = fmt.Sprintf("%s-sbom-%s.spdx.json", project.Slug, time.Now().Format("2006-01-02"))
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "GENERATE_FAILED", "message": err.Error()})
		return
	}

	data := document
	if preset == "technical-file" {
		envelope := gin.H{"sbom": json.RawMessage(document)}
		envelope["technical_file"] = sbom.BuildTechnicalFile(
			&project, components, options, format, ecosystem, time.Now())
		filename = fmt.Sprintf("%s-cra-technical-file-%s.json", project.Slug, time.Now().Format("2006-01-02"))
		encoded, err := json.MarshalIndent(envelope, "", "  ")
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": "GENERATE_FAILED", "message": err.Error()})
			return
		}
		data = encoded
	}

	// The detached signature covers the exact response body. It travels in
	// headers so the document itself stays valid JSON for its format.
	if sign {
		key, err := sbom.LoadSigningKey(h.compliance.SigningKeyFile)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": "SIGNING_UNAVAILABLE", "message": err.Error()})
			return
		}
		detached := sbom.SignDocument(key, data)
		c.Header("X-Depsilo-SBOM-Signature-Algorithm", detached.Algorithm)
		c.Header("X-Depsilo-SBOM-Signature", detached.Signature)
		c.Header("X-Depsilo-SBOM-Signature-PublicKey", detached.PublicKey)
		c.Header("X-Depsilo-SBOM-Signature-PayloadSHA256", detached.PayloadSHA256)
	}
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	c.Data(http.StatusOK, "application/json", data)
}

func queryFlag(c *gin.Context, name string) bool {
	switch strings.ToLower(strings.TrimSpace(c.Query(name))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
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
