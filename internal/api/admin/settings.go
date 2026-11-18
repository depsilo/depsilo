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

	"depsilo/internal/api/contract"
	"depsilo/internal/config"
)

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

func adminSettingsResponse(state config.SettingsState) contract.AdminSettingsResponse {
	var overrides map[contract.SettingPath]string
	if state.Overrides != nil {
		overrides = make(map[contract.SettingPath]string, len(state.Overrides))
		for path, name := range state.Overrides {
			overrides[contract.SettingPath(path)] = name
		}
	}
	var sources map[contract.SettingPath]contract.SettingSource
	if state.Sources != nil {
		sources = make(map[contract.SettingPath]contract.SettingSource, len(state.Sources))
		for path, source := range state.Sources {
			sources[contract.SettingPath(path)] = contract.SettingSource(source)
		}
	}
	return contract.AdminSettingsResponse{
		Configured:     settingsSnapshotContract(state.Configured),
		Effective:      settingsSnapshotContract(state.Effective),
		PendingRestart: editableSettingPaths(state.PendingRestart),
		Overrides:      overrides,
		Sources:        sources,
		Editable:       editableSettingPaths(state.Editable),
		ConfigWritable: state.ConfigWritable,
	}
}

func updateAdminSettingsResponse(result config.SettingsUpdateResult) contract.UpdateAdminSettingsResponse {
	return contract.UpdateAdminSettingsResponse{
		AdminSettingsResponse: adminSettingsResponse(result.SettingsState),
		Changed:               editableSettingPaths(result.Changed),
		AppliedNow:            editableSettingPaths(result.AppliedNow),
		RestartRequired:       editableSettingPaths(result.RestartRequired),
		BlockedByOverride:     editableSettingPaths(result.BlockedByOverride),
	}
}

func settingsSnapshotContract(s config.SettingsSnapshot) contract.AdminSettingsSnapshot {
	return contract.AdminSettingsSnapshot{
		Server:   contract.AdminSettingsServer{Host: s.Server.Host, Port: s.Server.Port, LogLevel: contract.AdminSettingsLogLevel(s.Server.LogLevel)},
		Database: contract.AdminSettingsDatabase{Driver: s.Database.Driver},
		Storage:  contract.AdminSettingsStorage{Type: s.Storage.Type, Path: s.Storage.Path},
		Cache: contract.AdminSettingsCache{
			MaxSizeGB: s.Cache.MaxSizeGB, TTLIndex: s.Cache.TTLIndex,
			TTLBlob: s.Cache.TTLBlob, LRUThreshold: s.Cache.LRUThreshold,
		},
		Auth: contract.AdminSettingsAuth{TokenTTL: s.Auth.TokenTTL},
	}
}

// editableSettingPaths copies a path list, keeping a nil list nil. Every list
// the settings store returns holds only editable paths.
func editableSettingPaths(paths []config.SettingPath) []contract.EditableSettingPath {
	if paths == nil {
		return nil
	}
	out := make([]contract.EditableSettingPath, len(paths))
	for i, path := range paths {
		out[i] = contract.EditableSettingPath(path)
	}
	return out
}

func writeBadSettingsRequest(c *gin.Context, err error) {
	c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": err.Error()})
}
