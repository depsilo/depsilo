package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"depsilo/internal/config"
)

type AdminSettingsResponse struct {
	Configured     config.SettingsSnapshot                     `json:"configured"`
	Effective      config.SettingsSnapshot                     `json:"effective"`
	PendingRestart []config.SettingPath                        `json:"pending_restart"`
	Overrides      map[config.SettingPath]string               `json:"overrides"`
	Sources        map[config.SettingPath]config.SettingSource `json:"sources"`
	Editable       []config.SettingPath                        `json:"editable"`
	ConfigWritable bool                                        `json:"config_writable"`
}

type UpdateAdminSettingsRequest struct {
	Server *struct {
		LogLevel *string `json:"log_level"`
	} `json:"server"`
	Cache *struct {
		MaxSizeGB    *int    `json:"max_size_gb"`
		TTLIndex     *string `json:"ttl_index"`
		TTLBlob      *string `json:"ttl_blob"`
		LRUThreshold *int    `json:"lru_threshold"`
	} `json:"cache"`
	Auth *struct {
		TokenTTL *string `json:"token_ttl"`
	} `json:"auth"`
}

type UpdateAdminSettingsResponse struct {
	AdminSettingsResponse
	Changed           []config.SettingPath `json:"changed"`
	AppliedNow        []config.SettingPath `json:"applied_now"`
	RestartRequired   []config.SettingPath `json:"restart_required"`
	BlockedByOverride []config.SettingPath `json:"blocked_by_override"`
}

type settingsStore interface {
	Snapshot(context.Context) (config.SettingsState, error)
	Update(context.Context, config.SettingsPatch) (config.SettingsUpdateResult, error)
}

type SettingsHandler struct {
	store settingsStore
}

// maxSettingsRequestBodyBytes bounds the small, fixed-shape Settings patch DTO.
const maxSettingsRequestBodyBytes int64 = 1 << 20

func NewSettingsHandler(store settingsStore) *SettingsHandler {
	return &SettingsHandler{store: store}
}

func (h *SettingsHandler) Get(c *gin.Context) {
	state, err := h.store.Snapshot(c.Request.Context())
	if err != nil {
		writeSettingsError(c, err)
		return
	}
	c.JSON(http.StatusOK, adminSettingsResponse(state))
}

func (h *SettingsHandler) Update(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxSettingsRequestBodyBytes)
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			writeBadSettingsRequest(c, errors.New("request body too large"))
			return
		}
		writeBadSettingsRequest(c, err)
		return
	}
	if err := validateSettingsJSON(body); err != nil {
		writeBadSettingsRequest(c, err)
		return
	}

	var request UpdateAdminSettingsRequest
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeBadSettingsRequest(c, err)
		return
	}
	if err := requireJSONEOF(decoder); err != nil {
		writeBadSettingsRequest(c, err)
		return
	}

	patch := request.settingsPatch()
	if settingsPatchIsEmpty(patch) {
		writeBadSettingsRequest(c, errors.New("settings patch must include at least one editable field"))
		return
	}

	result, err := h.store.Update(c.Request.Context(), patch)
	if err != nil {
		writeSettingsError(c, err)
		return
	}
	c.JSON(http.StatusOK, updateAdminSettingsResponse(result))
}

func validateSettingsJSON(body []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(body))
	if err := validateJSONValue(decoder, ""); err != nil {
		return err
	}
	return requireJSONEOF(decoder)
}

func validateJSONValue(decoder *json.Decoder, objectPath string) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	if token == nil {
		return errors.New("explicit null is not allowed in settings requests")
	}

	delim, isDelim := token.(json.Delim)
	if !isDelim {
		return nil
	}

	switch delim {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return errors.New("JSON object key must be a string")
			}
			if _, exists := seen[key]; exists {
				return fmt.Errorf("duplicate JSON object key %q", key)
			}
			seen[key] = struct{}{}
			if constrained, allowed := settingsJSONMemberAllowed(objectPath, key); constrained && !allowed {
				return fmt.Errorf("unknown settings field %q", settingsJSONPath(objectPath, key))
			}
			if err := validateJSONValue(decoder, settingsJSONPath(objectPath, key)); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil {
			return err
		}
		if closing != json.Delim('}') {
			return errors.New("malformed JSON object")
		}
	case '[':
		for decoder.More() {
			if err := validateJSONValue(decoder, objectPath); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil {
			return err
		}
		if closing != json.Delim(']') {
			return errors.New("malformed JSON array")
		}
	default:
		return errors.New("unexpected closing JSON delimiter")
	}
	return nil
}

func settingsJSONMemberAllowed(objectPath, member string) (bool, bool) {
	switch objectPath {
	case "":
		return true, member == "server" || member == "cache" || member == "auth"
	case "server":
		return true, member == "log_level"
	case "cache":
		return true, member == "max_size_gb" || member == "ttl_index" || member == "ttl_blob" || member == "lru_threshold"
	case "auth":
		return true, member == "token_ttl"
	default:
		return false, false
	}
}

func settingsJSONPath(parent, member string) string {
	if parent == "" {
		return member
	}
	return parent + "." + member
}

func (r UpdateAdminSettingsRequest) settingsPatch() config.SettingsPatch {
	var patch config.SettingsPatch
	if r.Server != nil && r.Server.LogLevel != nil {
		patch.Server = &config.SettingsServerPatch{LogLevel: r.Server.LogLevel}
	}
	if r.Cache != nil && (r.Cache.MaxSizeGB != nil || r.Cache.TTLIndex != nil || r.Cache.TTLBlob != nil || r.Cache.LRUThreshold != nil) {
		patch.Cache = &config.SettingsCachePatch{
			MaxSizeGB:    r.Cache.MaxSizeGB,
			TTLIndex:     r.Cache.TTLIndex,
			TTLBlob:      r.Cache.TTLBlob,
			LRUThreshold: r.Cache.LRUThreshold,
		}
	}
	if r.Auth != nil && r.Auth.TokenTTL != nil {
		patch.Auth = &config.SettingsAuthPatch{TokenTTL: r.Auth.TokenTTL}
	}
	return patch
}

func settingsPatchIsEmpty(patch config.SettingsPatch) bool {
	return patch.Server == nil && patch.Cache == nil && patch.Auth == nil
}

func requireJSONEOF(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("request body must contain exactly one JSON value")
		}
		return err
	}
	return nil
}

func adminSettingsResponse(state config.SettingsState) AdminSettingsResponse {
	return AdminSettingsResponse{
		Configured:     state.Configured,
		Effective:      state.Effective,
		PendingRestart: state.PendingRestart,
		Overrides:      state.Overrides,
		Sources:        state.Sources,
		Editable:       state.Editable,
		ConfigWritable: state.ConfigWritable,
	}
}

func updateAdminSettingsResponse(result config.SettingsUpdateResult) UpdateAdminSettingsResponse {
	return UpdateAdminSettingsResponse{
		AdminSettingsResponse: adminSettingsResponse(result.SettingsState),
		Changed:               result.Changed,
		AppliedNow:            result.AppliedNow,
		RestartRequired:       result.RestartRequired,
		BlockedByOverride:     result.BlockedByOverride,
	}
}

func writeBadSettingsRequest(c *gin.Context, err error) {
	c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": err.Error()})
}

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

type LogsHandler struct {
}
