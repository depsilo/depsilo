package admin

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"depsilo/internal/db"
	"depsilo/internal/packagepolicy"
	packagerules "depsilo/internal/rules"
	"depsilo/internal/security"
)

type SecurityHandler struct {
	db              *gorm.DB
	scanner         *security.Scanner
	catalog         *security.AdvisoryCatalog
	invalidateRules func()
	scanContext     context.Context
	scanTimeout     time.Duration
}

const maxAdvisoryImportRequestBytes = 33 << 20

// NewSecurityHandler creates the package-security admin handler. A nil
// invalidateRules callback is accepted; a non-nil callback runs after a
// manually approved package rule has committed successfully.
func NewSecurityHandler(
	database *gorm.DB,
	scanner *security.Scanner,
	catalog *security.AdvisoryCatalog,
	invalidateRules func(),
) *SecurityHandler {
	return NewSecurityHandlerWithContext(context.Background(), database, scanner, catalog, invalidateRules)
}

// NewSecurityHandlerWithContext binds manually-triggered scans to the server
// lifecycle instead of the request context, which is canceled as soon as the
// 202 response is returned.
func NewSecurityHandlerWithContext(
	scanContext context.Context,
	database *gorm.DB,
	scanner *security.Scanner,
	catalog *security.AdvisoryCatalog,
	invalidateRules func(),
) *SecurityHandler {
	if scanContext == nil {
		scanContext = context.Background()
	}
	if invalidateRules == nil {
		invalidateRules = func() {}
	}
	return &SecurityHandler{
		db:              database,
		scanner:         scanner,
		catalog:         catalog,
		invalidateRules: invalidateRules,
		scanContext:     scanContext,
		scanTimeout:     15 * time.Minute,
	}
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
		// Automatic blocking is safety-disabled until OSV affected sets have
		// a lossless native policy representation. A username is not machine
		// provenance and must never be counted as an automatic rule owner.
		AutoBlockedCount: 0,
		LastScanAt:       lastScanStr,
		ScanInProgress:   h.scanner.IsScanning(),
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
	if err := c.ShouldBindJSON(&body); err != nil && !errors.Is(err, io.EOF) {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": "request body must be valid JSON"})
		return
	}

	version := body.Version
	if strings.TrimSpace(version) == "" {
		c.JSON(http.StatusUnprocessableEntity, gin.H{
			"code":    "VERSION_REQUIRED",
			"message": "an explicit reviewed Package Rule version selector is required",
		})
		return
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
	if err := packagerules.NewStore(h.db).Create(&rule); err != nil {
		if errors.Is(err, packagepolicy.ErrInvalidRule) {
			c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"code": "CREATE_FAILED", "message": err.Error()})
		return
	}
	h.invalidateRules()
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
	err := h.scanner.StartScan(h.scanContext, h.scanTimeout)
	if errors.Is(err, security.ErrScanInProgress) {
		c.JSON(http.StatusConflict, gin.H{"code": "SCAN_IN_PROGRESS", "message": "scan already in progress"})
		return
	}
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "SCAN_UNAVAILABLE", "message": err.Error()})
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"status": "scan_started"})
}

func (h *SecurityHandler) ImportData(c *gin.Context) {
	if h.catalog == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"code": "IMPORT_UNAVAILABLE", "message": "advisory import is unavailable",
		})
		return
	}

	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxAdvisoryImportRequestBytes)
	file, _, err := c.Request.FormFile("file")
	if err != nil {
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{
				"code": "IMPORT_TOO_LARGE", "message": "advisory import exceeds the size limit",
			})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"code": "NO_FILE", "message": "no file uploaded"})
		return
	}
	defer func() {
		if err := file.Close(); err != nil {
			zap.L().Warn("close security advisory upload", zap.Error(err))
		}
	}()

	receipt, err := h.catalog.Import(c.Request.Context(), file)
	switch {
	case err == nil:
		c.JSON(http.StatusOK, gin.H{
			"imported":      receipt.Advisories,
			"received":      receipt.Received,
			"packages":      receipt.Packages,
			"duplicates":    receipt.Duplicates,
			"skipped":       receipt.Skipped,
			"rules_created": receipt.RulesCreated,
		})
	case errors.Is(err, security.ErrInvalidAdvisoryImport):
		c.JSON(http.StatusBadRequest, gin.H{
			"code": "INVALID_IMPORT", "message": "invalid advisory import",
		})
	case errors.Is(err, security.ErrAdvisoryImportTooLarge):
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{
			"code": "IMPORT_TOO_LARGE", "message": "advisory import exceeds the size limit",
		})
	default:
		zap.L().Error("import security advisories", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code": "IMPORT_FAILED", "message": "advisory import failed",
		})
	}
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
	ecosystem := strings.ToLower(strings.TrimSpace(c.Param("ecosystem")))

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
	autoBlockSupported, err := security.SupportsAutomaticVersionBlocking(ecosystem)
	if err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"code": "INVALID_POLICY", "message": err.Error()})
		return
	}
	if req.AutoBlockEnabled && !autoBlockSupported {
		c.JSON(http.StatusUnprocessableEntity, gin.H{
			"code":    "INVALID_POLICY",
			"message": "automatic vulnerability blocking is safety-disabled because OSV affected sets cannot be projected losslessly to Package Rule selectors",
		})
		return
	}

	policy := db.SecurityPolicy{
		Ecosystem: ecosystem, AutoBlockEnabled: req.AutoBlockEnabled,
		MinCVSSScore: minCVSSScore, CreatedBy: "admin",
	}
	result := h.db.Where("ecosystem = ?", ecosystem).Assign(map[string]any{
		"auto_block_enabled": req.AutoBlockEnabled,
		"min_cvss_score":     minCVSSScore,
		"created_by":         "admin",
	}).FirstOrCreate(&policy)
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "DB_ERROR", "message": result.Error.Error()})
		return
	}
	c.JSON(http.StatusOK, toSecurityPolicyResponse(policy))
}
