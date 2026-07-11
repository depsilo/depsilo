package admin

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"depsilo/internal/db"
	"depsilo/internal/security"
)

type SecurityHandler struct {
	db       *gorm.DB
	scanner  *security.Scanner
	importer *security.Importer
}

func NewSecurityHandler(database *gorm.DB, scanner *security.Scanner, importer *security.Importer) *SecurityHandler {
	return &SecurityHandler{db: database, scanner: scanner, importer: importer}
}

func (h *SecurityHandler) Dashboard(c *gin.Context) {
	var totalVulns int64
	if err := h.db.Model(&db.Vulnerability{}).Count(&totalVulns).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "DB_ERROR", "message": err.Error()})
		return
	}

	var affectedPkgs int64
	if err := h.db.Model(&db.VulnerabilityCheck{}).Where("has_vulnerabilities = ?", true).Count(&affectedPkgs).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "DB_ERROR", "message": err.Error()})
		return
	}

	type severityCount struct {
		Severity string
		Count    int64
	}
	var bySeverity []severityCount
	if err := h.db.Model(&db.Vulnerability{}).Select("severity, count(*) as count").Group("severity").Find(&bySeverity).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "DB_ERROR", "message": err.Error()})
		return
	}

	severityMap := map[string]int64{"critical": 0, "high": 0, "medium": 0, "low": 0}
	for _, s := range bySeverity {
		severityMap[s.Severity] = s.Count
	}

	var autoBlocked int64
	if err := h.db.Model(&db.PackageRule{}).Where("created_by = 'security-scanner'").Count(&autoBlocked).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "DB_ERROR", "message": err.Error()})
		return
	}

	lastScan := h.scanner.LastScanTime()
	var lastScanStr *string
	if !lastScan.IsZero() {
		s := lastScan.Format(time.RFC3339)
		lastScanStr = &s
	}

	c.JSON(http.StatusOK, securityDashboardResponse{
		TotalVulnerabilities: totalVulns,
		AffectedPackages:     affectedPkgs,
		BySeverity:           severityMap,
		AutoBlockedCount:     autoBlocked,
		LastScanAt:           lastScanStr,
		ScanInProgress:       h.scanner.IsScanning(),
	})
}

func (h *SecurityHandler) ListVulnerabilities(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	perPage, _ := strconv.Atoi(c.DefaultQuery("per_page", "20"))
	if page < 1 {
		page = 1
	}
	if perPage < 1 || perPage > 100 {
		perPage = 20
	}

	query := h.db.Model(&db.Vulnerability{})
	if eco := c.Query("ecosystem"); eco != "" {
		query = query.Where("ecosystem = ?", eco)
	}
	if sev := c.Query("severity"); sev != "" {
		query = query.Where("severity = ?", sev)
	}
	pkg := c.Query("package")
	_, packagePresent := c.Request.URL.Query()["package"]
	if !packagePresent {
		pkg = c.Query("q")
		if pkg != "" {
			zap.L().Warn("deprecated admin query parameter", zap.String("endpoint", "security/vulnerabilities"), zap.String("parameter", "q"), zap.String("replacement", "package"))
		}
	}
	if pkg != "" {
		query = query.Where("package_name LIKE ?", "%"+pkg+"%")
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "DB_ERROR", "message": err.Error()})
		return
	}

	var vulns []db.Vulnerability
	if err := query.Order("cvss_score DESC, published_at DESC").
		Offset((page - 1) * perPage).Limit(perPage).Find(&vulns).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "DB_ERROR", "message": err.Error()})
		return
	}

	c.JSON(http.StatusOK, securityPage[vulnerabilityResponse]{Items: toVulnerabilityResponses(vulns), Total: total, Page: page})
}

func (h *SecurityHandler) ListPackages(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	perPage, _ := strconv.Atoi(c.DefaultQuery("per_page", "20"))
	if page < 1 {
		page = 1
	}
	if perPage < 1 || perPage > 100 {
		perPage = 20
	}

	query := h.db.Model(&db.VulnerabilityCheck{}).Where("has_vulnerabilities = ?", true)
	if eco := c.Query("ecosystem"); eco != "" {
		query = query.Where("ecosystem = ?", eco)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "DB_ERROR", "message": err.Error()})
		return
	}

	var checks []db.VulnerabilityCheck
	if err := query.Order("vulnerability_count DESC").
		Offset((page - 1) * perPage).Limit(perPage).Find(&checks).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "DB_ERROR", "message": err.Error()})
		return
	}

	c.JSON(http.StatusOK, securityPage[vulnerabilityCheckResponse]{Items: toVulnerabilityCheckResponses(checks), Total: total, Page: page})
}

func (h *SecurityHandler) ListSuggestions(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	perPage, _ := strconv.Atoi(c.DefaultQuery("per_page", "20"))
	if page < 1 {
		page = 1
	}

	query := h.db.Model(&db.Vulnerability{}).
		Where("id NOT IN (?)", h.db.Model(&db.DismissedVuln{}).Select("vulnerability_id")).
		Where("cvss_score > 0").
		Order("cvss_score DESC")

	if eco := c.Query("ecosystem"); eco != "" {
		query = query.Where("ecosystem = ?", eco)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "DB_ERROR", "message": err.Error()})
		return
	}

	var vulns []db.Vulnerability
	if err := query.Offset((page - 1) * perPage).Limit(perPage).Find(&vulns).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "DB_ERROR", "message": err.Error()})
		return
	}

	c.JSON(http.StatusOK, securityPage[vulnerabilityResponse]{Items: toVulnerabilityResponses(vulns), Total: total, Page: page})
}

func (h *SecurityHandler) ApproveSuggestion(c *gin.Context) {
	vulnID, _ := strconv.Atoi(c.Param("vuln_id"))

	var vuln db.Vulnerability
	if err := h.db.First(&vuln, vulnID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "vulnerability not found"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"code": "DB_ERROR", "message": err.Error()})
		}
		return
	}

	var body struct {
		Version string `json:"version"`
		Reason  string `json:"reason"`
	}
	c.ShouldBindJSON(&body)

	version := body.Version
	if version == "" {
		version = security.FormatVersionConstraint(&vuln)
	}
	reason := body.Reason
	if reason == "" {
		reason = vuln.OSVID
		if vuln.CVSSScore > 0 {
			reason += " (CVSS " + strconv.FormatFloat(float64(vuln.CVSSScore), 'f', 1, 32) + ")"
		}
	}

	rule := db.PackageRule{
		Ecosystem: vuln.Ecosystem, PackageName: vuln.PackageName,
		Version: version, Action: "deny", Reason: reason, CreatedBy: "admin",
	}
	if err := h.db.Create(&rule).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "CREATE_FAILED", "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"rule_id": rule.ID})
}

func (h *SecurityHandler) DismissSuggestion(c *gin.Context) {
	vulnID, _ := strconv.Atoi(c.Param("vuln_id"))
	dismissed := db.DismissedVuln{VulnerabilityID: uint(vulnID), DismissedBy: "admin"}
	if err := h.db.Create(&dismissed).Error; err != nil {
		c.JSON(http.StatusConflict, gin.H{"code": "ALREADY_DISMISSED", "message": "already dismissed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "dismissed"})
}

func (h *SecurityHandler) TriggerScan(c *gin.Context) {
	if h.scanner.IsScanning() {
		c.JSON(http.StatusConflict, gin.H{"code": "SCAN_IN_PROGRESS", "message": "scan already in progress"})
		return
	}
	go func() {
		h.scanner.ScanAll(c.Request.Context())
	}()
	c.JSON(http.StatusAccepted, gin.H{"status": "scan_started"})
}

func (h *SecurityHandler) ImportData(c *gin.Context) {
	file, _, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "NO_FILE", "message": "no file uploaded"})
		return
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "READ_ERROR", "message": err.Error()})
		return
	}

	count, err := h.importer.Import(data)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "IMPORT_ERROR", "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"imported": count})
}

func (h *SecurityHandler) ListPolicies(c *gin.Context) {
	var policies []db.SecurityPolicy
	if err := h.db.Order("ecosystem").Find(&policies).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "DB_ERROR", "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, toSecurityPolicyResponses(policies))
}

func (h *SecurityHandler) UpdatePolicy(c *gin.Context) {
	ecosystem := c.Param("ecosystem")

	var req updateSecurityPolicyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": "invalid request body"})
		return
	}
	minCVSSScore, valid := req.validatedMinCVSSScore()
	if !valid {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"code": "INVALID_POLICY", "message": "min_cvss_score must be between 0 and 10"})
		return
	}

	policy := db.SecurityPolicy{
		Ecosystem: ecosystem, AutoBlockEnabled: req.AutoBlockEnabled,
		MinCVSSScore: minCVSSScore, CreatedBy: "admin",
	}
	result := h.db.Where("ecosystem = ?", ecosystem).Assign(policy).FirstOrCreate(&policy)
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "DB_ERROR", "message": result.Error.Error()})
		return
	}
	c.JSON(http.StatusOK, toSecurityPolicyResponse(policy))
}
