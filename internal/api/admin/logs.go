package admin

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"depsilo/internal/db"
)

const maxAccessLogExportRows = 10000

type AccessLogHandler struct {
	db *gorm.DB
}

func NewAccessLogHandler(database *gorm.DB) *AccessLogHandler {
	return &AccessLogHandler{db: database}
}

type accessLogFilter struct {
	Search      string
	AdapterType string
	Hit         *bool
	Page        int
	PageSize    int
}

type accessLogResponse struct {
	ID          uint      `json:"id"`
	AdapterType string    `json:"adapter_type"`
	Method      string    `json:"method"`
	CacheKey    string    `json:"cache_key"`
	PackageName string    `json:"package_name"`
	Hit         bool      `json:"hit"`
	Upstream    string    `json:"upstream"`
	LatencyMs   int64     `json:"latency_ms"`
	StatusCode  int       `json:"status_code"`
	ClientIP    string    `json:"client_ip"`
	BytesSent   int64     `json:"bytes_sent"`
	CreatedAt   time.Time `json:"created_at"`
}

type accessLogListResponse struct {
	Items    []accessLogResponse `json:"items"`
	Total    int64               `json:"total"`
	Page     int                 `json:"page"`
	PageSize int                 `json:"page_size"`
}

func parseAccessLogFilter(c *gin.Context) (accessLogFilter, error) {
	page, err := strconv.Atoi(c.DefaultQuery("page", "1"))
	if err != nil || page < 1 {
		page = 1
	}
	pageSize, err := strconv.Atoi(c.DefaultQuery("page_size", "50"))
	if err != nil || pageSize < 1 || pageSize > 200 {
		pageSize = 50
	}
	adapterType := c.Query("adapter_type")
	if adapterType == "" {
		adapterType = c.Query("type")
	}
	filter := accessLogFilter{
		Search:      c.Query("search"),
		AdapterType: adapterType,
		Page:        page,
		PageSize:    pageSize,
	}
	if raw := c.Query("hit"); raw != "" {
		hit, err := strconv.ParseBool(raw)
		if err != nil {
			return accessLogFilter{}, fmt.Errorf("hit must be true or false")
		}
		filter.Hit = &hit
	}
	return filter, nil
}

func applyAccessLogFilter(database *gorm.DB, filter accessLogFilter) *gorm.DB {
	query := database.Model(&db.AccessLog{})
	if filter.Search != "" {
		query = query.Where("package_name LIKE ?", "%"+filter.Search+"%")
	}
	if filter.AdapterType != "" {
		query = query.Where("adapter_type = ?", filter.AdapterType)
	}
	if filter.Hit != nil {
		query = query.Where("hit = ?", *filter.Hit)
	}
	return query
}

func toAccessLogResponses(items []db.AccessLog) []accessLogResponse {
	responses := make([]accessLogResponse, len(items))
	for i, item := range items {
		responses[i] = accessLogResponse{
			ID:          item.ID,
			AdapterType: item.AdapterType,
			Method:      item.Method,
			CacheKey:    item.CacheKey,
			PackageName: item.PackageName,
			Hit:         item.Hit,
			Upstream:    item.Upstream,
			LatencyMs:   item.LatencyMs,
			StatusCode:  item.StatusCode,
			ClientIP:    item.ClientIP,
			BytesSent:   item.BytesSent,
			CreatedAt:   item.CreatedAt,
		}
	}
	return responses
}

func (h *AccessLogHandler) List(c *gin.Context) {
	filter, err := parseAccessLogFilter(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": err.Error()})
		return
	}

	query := applyAccessLogFilter(h.db.WithContext(c.Request.Context()), filter)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "DB_ERROR", "message": err.Error()})
		return
	}

	var logs []db.AccessLog
	if err := query.Order("datetime(created_at) DESC").Offset((filter.Page - 1) * filter.PageSize).Limit(filter.PageSize).Find(&logs).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "DB_ERROR", "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, accessLogListResponse{
		Items:    toAccessLogResponses(logs),
		Total:    total,
		Page:     filter.Page,
		PageSize: filter.PageSize,
	})
}

func encodeAccessLogsCSV(items []accessLogResponse) ([]byte, error) {
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	if err := w.Write([]string{"Time", "Method", "Ecosystem", "Package", "Hit", "Status", "Latency(ms)", "Bytes", "Upstream", "Client IP", "Cache Key"}); err != nil {
		return nil, err
	}
	for _, item := range items {
		record := []string{
			item.CreatedAt.Format(time.RFC3339),
			item.Method,
			item.AdapterType,
			item.PackageName,
			strconv.FormatBool(item.Hit),
			strconv.Itoa(item.StatusCode),
			strconv.FormatInt(item.LatencyMs, 10),
			strconv.FormatInt(item.BytesSent, 10),
			item.Upstream,
			item.ClientIP,
			item.CacheKey,
		}
		for i := range record {
			record[i] = neutralizeCSVCell(record[i])
		}
		if err := w.Write(record); err != nil {
			return nil, err
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (h *AccessLogHandler) Export(c *gin.Context) {
	filter, err := parseAccessLogFilter(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": err.Error()})
		return
	}

	var logs []db.AccessLog
	query := applyAccessLogFilter(h.db.WithContext(c.Request.Context()), filter)
	if err := query.Order("datetime(created_at) DESC").Limit(maxAccessLogExportRows).Find(&logs).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "EXPORT_ERROR", "message": err.Error()})
		return
	}
	data, err := encodeAccessLogsCSV(toAccessLogResponses(logs))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "EXPORT_ERROR", "message": err.Error()})
		return
	}
	filename := fmt.Sprintf("depsilo-access-logs-%s.csv", time.Now().UTC().Format("2006-01-02"))
	c.Header("Content-Disposition", "attachment; filename=\""+filename+"\"")
	c.Data(http.StatusOK, "text/csv; charset=utf-8", data)
}
