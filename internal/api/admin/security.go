package admin

import (
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
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
	h.db.Model(&db.Vulnerability{}).Count(&totalVulns)

	var affectedPkgs int64
	h.db.Model(&db.VulnerabilityCheck{}).Where("has_vulnerabilities = ?", true).Count(&affectedPkgs)

	type severityCount struct {
		Severity string
		Count    int64
	}
	var bySeverity []severityCount
	h.db.Model(&db.Vulnerability{}).Select("severity, count(*) as count").Group("severity").Find(&bySeverity)

	severityMap := map[string]int64{"critical": 0, "high": 0, "medium": 0, "low": 0}
	for _, s := range bySeverity {
		severityMap[s.Severity] = s.Count
	}

	var autoBlocked int64
	h.db.Model(&db.PackageRule{}).Where("created_by = 'security-scanner'").Count(&autoBlocked)

	lastScan := h.scanner.LastScanTime()
	var lastScanStr *string
	if !lastScan.IsZero() {
		s := lastScan.Format(time.RFC3339)
		lastScanStr = &s
	}

	c.JSON(http.StatusOK, gin.H{
		"total_vulnerabilities": totalVulns,
		"affected_packages":     affectedPkgs,
		"by_severity":           severityMap,
		"auto_blocked_count":    autoBlocked,
		"last_scan_at":          lastScanStr,
		"scan_in_progress":      h.scanner.IsScanning(),
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
	if pkg := c.Query("package"); pkg != "" {
		query = query.Where("package_name LIKE ?", "%"+pkg+"%")
	}

	var total int64
	query.Count(&total)

	var vulns []db.Vulnerability
	query.Order("cvss_score DESC, published_at DESC").
		Offset((page - 1) * perPage).Limit(perPage).Find(&vulns)

	c.JSON(http.StatusOK, gin.H{"items": vulns, "total": total, "page": page})
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
	query.Count(&total)

	var checks []db.VulnerabilityCheck
	query.Order("vulnerability_count DESC").
		Offset((page - 1) * perPage).Limit(perPage).Find(&checks)

	c.JSON(http.StatusOK, gin.H{"items": checks, "total": total, "page": page})
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
	query.Count(&total)

	var vulns []db.Vulnerability
	query.Offset((page - 1) * perPage).Limit(perPage).Find(&vulns)

	c.JSON(http.StatusOK, gin.H{"items": vulns, "total": total, "page": page})
}

func (h *SecurityHandler) ApproveSuggestion(c *gin.Context) {
	vulnID, _ := strconv.Atoi(c.Param("vuln_id"))

	var vuln db.Vulnerability
	if err := h.db.First(&vuln, vulnID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "vulnerability not found"})
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
	h.db.Order("ecosystem").Find(&policies)
	c.JSON(http.StatusOK, policies)
}

func (h *SecurityHandler) UpdatePolicy(c *gin.Context) {
	ecosystem := c.Param("ecosystem")

	var body struct {
		AutoBlockEnabled bool    `json:"auto_block_enabled"`
		MinCVSSScore     float32 `json:"min_cvss_score"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_BODY", "message": err.Error()})
		return
	}

	policy := db.SecurityPolicy{
		Ecosystem: ecosystem, AutoBlockEnabled: body.AutoBlockEnabled,
		MinCVSSScore: body.MinCVSSScore, CreatedBy: "admin",
	}
	result := h.db.Where("ecosystem = ?", ecosystem).Assign(policy).FirstOrCreate(&policy)
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "UPDATE_FAILED", "message": result.Error.Error()})
		return
	}
	c.JSON(http.StatusOK, policy)
}
