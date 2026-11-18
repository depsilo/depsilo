package admin

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"depsilo/internal/config"
)

func writeSettingsError(c *gin.Context, err error) {
	var storeErr *config.StoreError
	if !errors.As(err, &storeErr) {
		writeInternalSettingsError(c, err)
		return
	}

	status := http.StatusInternalServerError
	switch storeErr.Code {
	case config.StoreInvalidSetting:
		status = http.StatusUnprocessableEntity
	case config.StoreAuthenticationSourceNotFound:
		status = http.StatusNotFound
	case config.StoreAuthenticationSourceConflict:
		status = http.StatusConflict
	case config.StoreConfigReadOnly:
		status = http.StatusConflict
	case config.StoreConfigReadFailed, config.StoreConfigWriteFailed:
	default:
		writeInternalSettingsError(c, err)
		return
	}
	c.JSON(status, gin.H{"code": string(storeErr.Code), "message": storeErr.Error()})
}

func writeInternalSettingsError(c *gin.Context, err error) {
	zap.L().Error("settings request failed", zap.Error(err))
	c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL_ERROR", "message": "internal server error"})
}
