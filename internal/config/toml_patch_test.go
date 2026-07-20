package config

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestPatchSettingsDocumentPreservesUntouchedBytes(t *testing.T) {
	input := []byte("# operator header\r\n[server]\r\nhost  = '127.0.0.1' # keep\r\nlog_level = \"info\" # level\r\n\r\n[cache]\r\nmax_size_gb = 20\r\nttl_index = \"5m\"\r\n# blob policy\r\nttl_blob   = \"72h\"\r\nlru_threshold = 90\r\n\r\n[custom]\r\nverbatim = { x = 1 }\r\n")
	level, blob := "warn", "96h"
	got, explicit, err := patchSettingsDocument(input, SettingsPatch{Server: &SettingsServerPatch{LogLevel: &level}, Cache: &SettingsCachePatch{TTLBlob: &blob}})
	if err != nil {
		t.Fatal(err)
	}
	want := []byte("# operator header\r\n[server]\r\nhost  = '127.0.0.1' # keep\r\nlog_level = \"warn\" # level\r\n\r\n[cache]\r\nmax_size_gb = 20\r\nttl_index = \"5m\"\r\n# blob policy\r\nttl_blob   = \"96h\"\r\nlru_threshold = 90\r\n\r\n[custom]\r\nverbatim = { x = 1 }\r\n")
	if !bytes.Equal(got, want) {
		t.Fatalf("patched document:\n%s\nwant:\n%s", got, want)
	}
	if !explicit[SettingServerLogLevel] || !explicit[SettingCacheTTLBlob] {
		t.Fatalf("explicit = %#v", explicit)
	}
}

func TestPatchSettingsDocumentAddsOnlyMissingKeys(t *testing.T) {
	input := []byte("[server]\nhost = \"0.0.0.0\"\n\n[custom]\nkeep = true\n")
	level, ttl := "debug", "24h"
	got, _, err := patchSettingsDocument(input, SettingsPatch{Server: &SettingsServerPatch{LogLevel: &level}, Auth: &SettingsAuthPatch{TokenTTL: &ttl}})
	if err != nil {
		t.Fatal(err)
	}
	want := "auth.token_ttl = \"24h\"\n[server]\nhost = \"0.0.0.0\"\nlog_level = \"debug\"\n\n[custom]\nkeep = true\n"
	if string(got) != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestPatchSettingsDocumentRejectsMalformedCurrentTOML(t *testing.T) {
	level := "warn"
	if _, _, err := patchSettingsDocument([]byte("[server\n"), SettingsPatch{Server: &SettingsServerPatch{LogLevel: &level}}); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestPatchSettingsDocumentReplacesRootDottedKey(t *testing.T) {
	input := []byte("server.log_level = 'info' # keep\n[custom]\nkeep = true\n")
	level := "error"
	got, explicit, err := patchSettingsDocument(input, SettingsPatch{Server: &SettingsServerPatch{LogLevel: &level}})
	if err != nil {
		t.Fatal(err)
	}
	want := "server.log_level = \"error\" # keep\n[custom]\nkeep = true\n"
	if string(got) != want || !explicit[SettingServerLogLevel] {
		t.Fatalf("got/explicit:\n%s\n%#v", got, explicit)
	}
}

func TestPatchSettingsDocumentPreservesQuotedLiteralDottedKey(t *testing.T) {
	input := []byte("\"server.log_level\" = \"custom\"\n")
	level := "warn"
	got, explicit, err := patchSettingsDocument(input, SettingsPatch{Server: &SettingsServerPatch{LogLevel: &level}})
	if err != nil {
		t.Fatal(err)
	}
	want := "\"server.log_level\" = \"custom\"\nserver.log_level = \"warn\"\n"
	if string(got) != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
	if !explicit[SettingServerLogLevel] {
		t.Fatalf("explicit = %#v", explicit)
	}
	cfg, err := decodeConfigDocument(got)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.LogLevel != "warn" {
		t.Fatalf("decoded log level = %q, want warn", cfg.Server.LogLevel)
	}
}

func TestLoadIgnoresQuotedLiteralDottedKeyAndPreservesEnvPrecedence(t *testing.T) {
	document := []byte("\"server.log_level\" = \"custom\"\nserver.log_level = \"warn\"\n")
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, document, 0o600); err != nil {
		t.Fatal(err)
	}

	t.Run("canonical file setting", func(t *testing.T) {
		setTestJWTSecret(t)
		t.Setenv("DEPSILO_CONFIG", path)
		cfg, err := Load()
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Server.LogLevel != "warn" {
			t.Fatalf("loaded log level = %q, want warn", cfg.Server.LogLevel)
		}
	})

	t.Run("environment override", func(t *testing.T) {
		setTestJWTSecret(t)
		t.Setenv("DEPSILO_CONFIG", path)
		t.Setenv("DEPSILO_SERVER_LOG_LEVEL", "error")
		cfg, err := Load()
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Server.LogLevel != "error" {
			t.Fatalf("loaded log level = %q, want error", cfg.Server.LogLevel)
		}
	})

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, document) {
		t.Fatalf("Load mutated config document:\n%s\nwant:\n%s", got, document)
	}
}

func TestDecodeConfigDocumentPreservesCanonicalKeyFormsBesideQuotedLiteral(t *testing.T) {
	tests := []struct {
		name     string
		document string
	}{
		{
			name:     "root dotted",
			document: "\"server.log_level\" = \"custom\"\nserver.log_level = \"warn\"\n",
		},
		{
			name:     "individually quoted dotted components",
			document: "\"server.log_level\" = \"custom\"\n\"server\".\"log_level\" = \"warn\"\n",
		},
		{
			name:     "standard table",
			document: "\"server.log_level\" = \"custom\"\n[server]\nlog_level = \"warn\"\n",
		},
		{
			name:     "inline table",
			document: "\"server.log_level\" = \"custom\"\nserver = { log_level = \"warn\" }\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := decodeConfigDocument([]byte(tt.document))
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Server.LogLevel != "warn" {
				t.Fatalf("decoded log level = %q, want warn", cfg.Server.LogLevel)
			}
		})
	}
}

func TestDecodeAndLoadIgnoreAllRootLiteralDottedKeys(t *testing.T) {
	document := []byte("\"database.dsn\" = \"literal\"\n\"custom.feature\" = { enabled = true }\ndatabase.dsn = \"canonical\"\nserver.log_level = \"warn\"\n")
	original := append([]byte(nil), document...)
	sanitized, err := sanitizeConfigDocumentForViper(document)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sanitized, []byte("\"database.dsn\"")) || bytes.Contains(sanitized, []byte("\"custom.feature\"")) {
		t.Fatalf("literal dotted root keys survived sanitizer:\n%s", sanitized)
	}
	if !bytes.Equal(document, original) {
		t.Fatalf("sanitizer mutated source:\n%s\nwant:\n%s", document, original)
	}

	cfg, err := decodeConfigDocument(document)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Database.DSN != "canonical" {
		t.Fatalf("decoded database.dsn = %q, want canonical", cfg.Database.DSN)
	}

	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, document, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DEPSILO_CONFIG", path)
	setTestJWTSecret(t)
	loaded, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Database.DSN != "canonical" {
		t.Fatalf("loaded database.dsn = %q, want canonical", loaded.Database.DSN)
	}
	onDisk, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(onDisk, document) {
		t.Fatalf("Load mutated document:\n%s\nwant:\n%s", onDisk, document)
	}
}

func TestDecodeAndLoadIgnoreMultilineLiteralDottedRootValues(t *testing.T) {
	tests := []struct {
		name     string
		document string
	}{
		{
			name: "multiline array",
			document: "\"database.dsn\" = [\n" +
				"  \"literal\",\n" +
				"  \"other\",\n" +
				"]\n" +
				"database.dsn = \"canonical\"\n" +
				"server.log_level = \"warn\"\n",
		},
		{
			name: "multiline inline containers",
			document: "\"custom.containers\" = [\n" +
				"  { name = \"first\", nested = { enabled = true } },\n" +
				"  { name = \"second\", values = [1, 2] },\n" +
				"]\n" +
				"database.dsn = \"canonical\"\n" +
				"server.log_level = \"warn\"\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setTestJWTSecret(t)
			document := []byte(tt.document)
			cfg, err := decodeConfigDocument(document)
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Database.DSN != "canonical" || cfg.Server.LogLevel != "warn" {
				t.Fatalf("decoded database/log level = %q/%q", cfg.Database.DSN, cfg.Server.LogLevel)
			}

			path := filepath.Join(t.TempDir(), "config.toml")
			if err := os.WriteFile(path, document, 0o600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("DEPSILO_CONFIG", path)
			setTestJWTSecret(t)
			loaded, err := Load()
			if err != nil {
				t.Fatal(err)
			}
			if loaded.Database.DSN != "canonical" || loaded.Server.LogLevel != "warn" {
				t.Fatalf("loaded database/log level = %q/%q", loaded.Database.DSN, loaded.Server.LogLevel)
			}
			onDisk, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(onDisk, document) {
				t.Fatalf("Load mutated document:\n%s\nwant:\n%s", onDisk, document)
			}
		})
	}
}

func TestPatchDecodeAndLoadIgnoreLiteralDottedTableBlocks(t *testing.T) {
	tests := []struct {
		name  string
		block string
	}{
		{
			name:  "standard table and subtable",
			block: "[\"server.log_level\"]\ncustom = [\n  \"keep\",\n  { nested = [1, 2] },\n]\n[\"server.log_level\".child]\nmore = \"keep too\"\n[cache]\nmax_size_gb = 8\n",
		},
		{
			name:  "array of tables",
			block: "[[\"server.log_level\"]]\ncustom = [\n  \"first\",\n  \"entry\",\n]\n[[\"server.log_level\"]]\ncustom = [\n  { name = \"second\" },\n]\n[\"server.log_level\".metadata]\nnote = \"keep\"\n[cache]\nmax_size_gb = 8\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			level := "warn"
			got, explicit, err := patchSettingsDocument([]byte(tt.block), SettingsPatch{Server: &SettingsServerPatch{LogLevel: &level}})
			if err != nil {
				t.Fatal(err)
			}
			want := "server.log_level = \"warn\"\n" + tt.block
			if string(got) != want {
				t.Fatalf("patched document:\n%s\nwant:\n%s", got, want)
			}
			if !explicit[SettingServerLogLevel] {
				t.Fatalf("explicit = %#v", explicit)
			}
			sanitized, err := sanitizeConfigDocumentForViper(got)
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Contains(sanitized, []byte("custom =")) || bytes.Contains(sanitized, []byte("more =")) || bytes.Contains(sanitized, []byte("note =")) {
				t.Fatalf("literal table children were promoted into Viper input:\n%s", sanitized)
			}
			cfg, err := decodeConfigDocument(got)
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Server.LogLevel != "warn" {
				t.Fatalf("decoded log level = %q, want warn", cfg.Server.LogLevel)
			}
			if cfg.Cache.MaxSizeGB != 8 {
				t.Fatalf("decoded cache.max_size_gb = %d, want 8", cfg.Cache.MaxSizeGB)
			}

			path := filepath.Join(t.TempDir(), "config.toml")
			if err := os.WriteFile(path, got, 0o600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("DEPSILO_CONFIG", path)
			setTestJWTSecret(t)
			loaded, err := Load()
			if err != nil {
				t.Fatal(err)
			}
			if loaded.Server.LogLevel != "warn" {
				t.Fatalf("loaded log level = %q, want warn", loaded.Server.LogLevel)
			}
			if loaded.Cache.MaxSizeGB != 8 {
				t.Fatalf("loaded cache.max_size_gb = %d, want 8", loaded.Cache.MaxSizeGB)
			}
			onDisk, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(onDisk, got) {
				t.Fatalf("Load mutated document:\n%s\nwant:\n%s", onDisk, got)
			}
		})
	}
}

func TestPatchSettingsDocumentAddsMissingKeyBeforeTrailingSectionComments(t *testing.T) {
	input := []byte("[cache]\nmax_size_gb = 20\n# retained for operator\n\n[custom]\nkeep = true\n")
	ttl := "10m"
	got, _, err := patchSettingsDocument(input, SettingsPatch{Cache: &SettingsCachePatch{TTLIndex: &ttl}})
	if err != nil {
		t.Fatal(err)
	}
	want := "[cache]\nmax_size_gb = 20\nttl_index = \"10m\"\n# retained for operator\n\n[custom]\nkeep = true\n"
	if string(got) != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestPatchSettingsDocumentReplacesInlineTableScalar(t *testing.T) {
	input := []byte("cache = { ttl_blob = '72h', max_size_gb = 20 } # keep\n")
	blob := "48h"
	got, explicit, err := patchSettingsDocument(input, SettingsPatch{Cache: &SettingsCachePatch{TTLBlob: &blob}})
	if err != nil {
		t.Fatal(err)
	}
	want := "cache = { ttl_blob = \"48h\", max_size_gb = 20 } # keep\n"
	if string(got) != want || !explicit[SettingCacheTTLBlob] {
		t.Fatalf("got/explicit:\n%s\n%#v", got, explicit)
	}
}

func TestPatchSettingsDocumentRejectsMissingChildOfInlineTable(t *testing.T) {
	input := []byte("cache = { max_size_gb = 20 }\n")
	ttl := "5m"
	if _, _, err := patchSettingsDocument(input, SettingsPatch{Cache: &SettingsCachePatch{TTLIndex: &ttl}}); err == nil {
		t.Fatal("expected sealed inline table error")
	}
}

func TestPatchSettingsDocumentAddsAfterSectionWithoutFinalNewline(t *testing.T) {
	input := []byte("[server]\nhost = \"0.0.0.0\"")
	level := "warn"
	got, _, err := patchSettingsDocument(input, SettingsPatch{Server: &SettingsServerPatch{LogLevel: &level}})
	if err != nil {
		t.Fatal(err)
	}
	want := "[server]\nhost = \"0.0.0.0\"\nlog_level = \"warn\"\n"
	if string(got) != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}
