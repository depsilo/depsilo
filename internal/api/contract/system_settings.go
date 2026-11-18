package contract

// SettingPath names one setting shown on the Settings page.
type SettingPath string

const (
	SettingPathServerHost        SettingPath = "server.host"
	SettingPathServerPort        SettingPath = "server.port"
	SettingPathServerLogLevel    SettingPath = "server.log_level"
	SettingPathDatabaseDriver    SettingPath = "database.driver"
	SettingPathStorageType       SettingPath = "storage.type"
	SettingPathStoragePath       SettingPath = "storage.path"
	SettingPathCacheMaxSizeGB    SettingPath = "cache.max_size_gb"
	SettingPathCacheTTLIndex     SettingPath = "cache.ttl_index"
	SettingPathCacheTTLBlob      SettingPath = "cache.ttl_blob"
	SettingPathCacheLRUThreshold SettingPath = "cache.lru_threshold"
	SettingPathAuthTokenTTL      SettingPath = "auth.token_ttl"
)

// EditableSettingPath is the subset of SettingPath that PUT /admin/settings
// accepts. Every path list in a settings body holds only these.
type EditableSettingPath string

const (
	EditableSettingPathServerLogLevel    EditableSettingPath = "server.log_level"
	EditableSettingPathCacheMaxSizeGB    EditableSettingPath = "cache.max_size_gb"
	EditableSettingPathCacheTTLIndex     EditableSettingPath = "cache.ttl_index"
	EditableSettingPathCacheTTLBlob      EditableSettingPath = "cache.ttl_blob"
	EditableSettingPathCacheLRUThreshold EditableSettingPath = "cache.lru_threshold"
	EditableSettingPathAuthTokenTTL      EditableSettingPath = "auth.token_ttl"
)

// SettingSource is where a setting's configured value comes from.
type SettingSource string

const (
	SettingSourceDefault SettingSource = "default"
	SettingSourceFile    SettingSource = "file"
	SettingSourceEnv     SettingSource = "env"
)

// AdminSettingsLogLevel is a validated server log level.
type AdminSettingsLogLevel string

const (
	AdminSettingsLogLevelDebug AdminSettingsLogLevel = "debug"
	AdminSettingsLogLevelInfo  AdminSettingsLogLevel = "info"
	AdminSettingsLogLevelWarn  AdminSettingsLogLevel = "warn"
	AdminSettingsLogLevelError AdminSettingsLogLevel = "error"
)

// AdminSettingsServer is the server section of a settings snapshot.
type AdminSettingsServer struct {
	Host     string                `json:"host"`
	Port     int                   `json:"port"`
	LogLevel AdminSettingsLogLevel `json:"log_level"`
}

// AdminSettingsDatabase is the database section of a settings snapshot.
type AdminSettingsDatabase struct {
	Driver string `json:"driver"`
}

// AdminSettingsStorage is the storage section of a settings snapshot.
type AdminSettingsStorage struct {
	Type string `json:"type"`
	Path string `json:"path"`
}

// AdminSettingsCache is the cache section of a settings snapshot. Durations
// are Go duration strings in their shortest form, such as "72h".
type AdminSettingsCache struct {
	MaxSizeGB    int    `json:"max_size_gb"`
	TTLIndex     string `json:"ttl_index"`
	TTLBlob      string `json:"ttl_blob"`
	LRUThreshold int    `json:"lru_threshold"`
}

// AdminSettingsAuth is the auth section of a settings snapshot.
type AdminSettingsAuth struct {
	TokenTTL string `json:"token_ttl"`
}

// AdminSettingsSnapshot is one full set of setting values, either as the
// config file declares them or as the running process uses them.
type AdminSettingsSnapshot struct {
	Server   AdminSettingsServer   `json:"server"`
	Database AdminSettingsDatabase `json:"database"`
	Storage  AdminSettingsStorage  `json:"storage"`
	Cache    AdminSettingsCache    `json:"cache"`
	Auth     AdminSettingsAuth     `json:"auth"`
}

// AdminSettingsResponse answers GET /admin/settings. Sources has every
// SettingPath; Overrides has only the paths an environment variable pins,
// mapped to that variable's name.
type AdminSettingsResponse struct {
	Configured     AdminSettingsSnapshot         `json:"configured"`
	Effective      AdminSettingsSnapshot         `json:"effective"`
	PendingRestart []EditableSettingPath         `json:"pending_restart"`
	Overrides      map[SettingPath]string        `json:"overrides" tstype:"{ [K in SettingPath]?: string }"`
	Sources        map[SettingPath]SettingSource `json:"sources" tstype:"{ [K in SettingPath]: SettingSource }"`
	Editable       []EditableSettingPath         `json:"editable"`
	ConfigWritable bool                          `json:"config_writable"`
}

// UpdateAdminSettingsResponse answers PUT /admin/settings: the new state plus
// what the patch changed and when each change takes effect.
type UpdateAdminSettingsResponse struct {
	AdminSettingsResponse `tstype:",extends"`
	Changed               []EditableSettingPath `json:"changed"`
	AppliedNow            []EditableSettingPath `json:"applied_now"`
	RestartRequired       []EditableSettingPath `json:"restart_required"`
	BlockedByOverride     []EditableSettingPath `json:"blocked_by_override"`
}
