package config

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDecodeAndLoadRejectRootLiteralDottedKey(t *testing.T) {
	document := []byte("\"database.dsn\" = \"./wrong.db\"\n[database]\ndsn = \"./depsilo.db\"\n")

	_, err := decodeConfigDocument(document)
	if err == nil {
		t.Fatal("decodeConfigDocument accepted a root literal dotted key")
	}
	if !strings.Contains(err.Error(), "database.dsn") || !strings.Contains(err.Error(), "literal dotted") {
		t.Fatalf("decodeConfigDocument error = %q, want the ambiguous key and an actionable explanation", err)
	}

	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, document, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DEPSILO_CONFIG", path)
	setTestJWTSecret(t)

	_, err = Load()
	if err == nil {
		t.Fatal("Load accepted a root literal dotted key")
	}
	if !strings.Contains(err.Error(), "database.dsn") || !strings.Contains(err.Error(), "literal dotted") {
		t.Fatalf("Load error = %q, want the ambiguous key and an actionable explanation", err)
	}
}

func TestDecodeRejectsRootLiteralDottedTable(t *testing.T) {
	for _, document := range []string{
		"[\"auth.jwt_secret\"]\nvalue = \"wrong\"\n",
		"[[\"auth.jwt_secret\"]]\nvalue = \"wrong\"\n",
	} {
		_, err := decodeConfigDocument([]byte(document))
		if err == nil {
			t.Fatalf("decodeConfigDocument accepted root literal dotted table:\n%s", document)
		}
		if !strings.Contains(err.Error(), "auth.jwt_secret") || !strings.Contains(err.Error(), "literal dotted table") {
			t.Fatalf("decodeConfigDocument error = %q, want the ambiguous table and an actionable explanation", err)
		}
	}
}

func TestDecodeAcceptsCanonicalDottedSyntaxAndCustomLiteralDottedContent(t *testing.T) {
	document := []byte(`config_version = 1
database.dsn = "./depsilo.db"
"server"."log_level" = "warn"

[custom]
"database.dsn" = "opaque"
containers = [
  { name = "first", nested = { enabled = true } },
  { name = "second", values = [1, 2] },
]

[custom."auth.jwt_secret"]
note = "operator owned"
`)

	cfg, err := decodeConfigDocument(document)
	if err != nil {
		t.Fatalf("decodeConfigDocument rejected valid dotted syntax or [custom] content: %v", err)
	}
	if cfg.Database.DSN != "./depsilo.db" || cfg.Server.LogLevel != "warn" {
		t.Fatalf("decoded database/log level = %q/%q", cfg.Database.DSN, cfg.Server.LogLevel)
	}
	if len(cfg.Custom) == 0 {
		t.Fatal("[custom] content was not decoded")
	}
}

func TestValidateDocumentAllowsLiteralDottedShapesInsideCustom(t *testing.T) {
	for _, tt := range []struct {
		name     string
		document string
	}{
		{name: "table key", document: "[custom]\n\"database.dsn\" = \"opaque\"\n"},
		{name: "quoted subtable", document: "[custom.\"auth.jwt_secret\"]\nnote = \"operator owned\"\n"},
		{name: "quoted array subtable", document: "[[custom.\"worker.pool\"]]\n\"field.name\" = \"opaque\"\n"},
		{name: "root dotted custom value", document: "custom.\"feature.flag\" = true\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidateDocument([]byte(tt.document)); err != nil {
				t.Fatalf("ValidateDocument rejected [custom] content:\n%s\nerror: %v", tt.document, err)
			}
		})
	}
}

func TestDecodeRejectsFutureVersionBeforeLiteralDottedSyntax(t *testing.T) {
	document := []byte("config_version = 999\n\"database.dsn\" = \"./wrong.db\"\n")

	_, err := decodeConfigDocument(document)
	if err == nil {
		t.Fatal("decodeConfigDocument accepted a future config version")
	}
	if !strings.Contains(err.Error(), "newer than this binary supports") || strings.Contains(err.Error(), "literal dotted") {
		t.Fatalf("decodeConfigDocument error = %q, want version refusal before strict syntax validation", err)
	}
}

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

func TestPatchSettingsDocumentRejectsRootLiteralDottedKey(t *testing.T) {
	input := []byte("\"server.log_level\" = \"custom\"\n")
	level := "warn"
	_, _, err := patchSettingsDocument(input, SettingsPatch{Server: &SettingsServerPatch{LogLevel: &level}})
	if err == nil {
		t.Fatal("patchSettingsDocument accepted a root literal dotted key")
	}
	if !strings.Contains(err.Error(), "server.log_level") || !strings.Contains(err.Error(), "literal dotted key") {
		t.Fatalf("patchSettingsDocument error = %q, want the ambiguous key and an actionable explanation", err)
	}
}

func TestLoadRejectsRootLiteralDottedKeyBeforeEnvironmentOverride(t *testing.T) {
	document := []byte("\"server.log_level\" = \"custom\"\nserver.log_level = \"warn\"\n")
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, document, 0o600); err != nil {
		t.Fatal(err)
	}
	setTestJWTSecret(t)
	t.Setenv("DEPSILO_CONFIG", path)
	t.Setenv("DEPSILO_SERVER_LOG_LEVEL", "error")

	_, err := Load()
	if err == nil {
		t.Fatal("Load allowed an environment override to mask a root literal dotted key")
	}
	if !strings.Contains(err.Error(), "server.log_level") || !strings.Contains(err.Error(), "literal dotted key") {
		t.Fatalf("Load error = %q, want the ambiguous file key named", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, document) {
		t.Fatalf("Load mutated config document:\n%s\nwant:\n%s", got, document)
	}
}

func TestDecodeConfigDocumentAcceptsCanonicalKeyForms(t *testing.T) {
	tests := []struct {
		name     string
		document string
	}{
		{
			name:     "root dotted",
			document: "server.log_level = \"warn\"\n",
		},
		{
			name:     "individually quoted dotted components",
			document: "\"server\".\"log_level\" = \"warn\"\n",
		},
		{
			name:     "standard table",
			document: "[server]\nlog_level = \"warn\"\n",
		},
		{
			name:     "inline table",
			document: "server = { log_level = \"warn\" }\n",
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

func TestValidateDocumentRejectsLiteralDottedKeyShapes(t *testing.T) {
	tests := []struct {
		name     string
		document string
	}{
		{
			name:     "basic quoted key",
			document: "\"database.dsn\" = \"literal\"\ndatabase.dsn = \"canonical\"\n",
		},
		{
			name:     "single quoted key",
			document: "'auth.jwt_secret' = \"literal\"\n",
		},
		{
			name:     "custom lookalike stays outside extension table",
			document: "\"custom.feature\" = { enabled = true }\n",
		},
		{
			name:     "literal dotted component in dotted key",
			document: "database.\"dsn.shadow\" = \"literal\"\n",
		},
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
			err := ValidateDocument([]byte(tt.document))
			if err == nil {
				t.Fatalf("ValidateDocument accepted literal dotted key:\n%s", tt.document)
			}
			if !strings.Contains(err.Error(), "literal dotted key") {
				t.Fatalf("ValidateDocument error = %q, want actionable literal dotted key error", err)
			}
		})
	}
}

func TestValidateDocumentRejectsLiteralDottedTableBlocks(t *testing.T) {
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
		{
			name:  "literal dotted component",
			block: "[auth.\"jwt.secret\"]\nvalue = \"wrong\"\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateDocument([]byte(tt.block))
			if err == nil {
				t.Fatalf("ValidateDocument accepted literal dotted table:\n%s", tt.block)
			}
			if !strings.Contains(err.Error(), "literal dotted table") {
				t.Fatalf("ValidateDocument error = %q, want actionable literal dotted table error", err)
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
