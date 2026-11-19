package config

import (
	"strconv"
	"strings"
	"testing"
)

func TestDecodeConfigDocumentRejectsInvalidExtraIndexDocuments(t *testing.T) {
	for _, testCase := range []struct {
		name     string
		document string
		wantErr  string
	}{
		{
			name: "empty path",
			document: `
[[extra_indexes]]
name = "private"
path = " / "
`,
			wantErr: "extra_indexes[0].path must not be empty",
		},
		{
			name: "duplicate paths",
			document: `
[[extra_indexes]]
name = "private-a"
path = "python/private"

[[extra_indexes]]
name = "private-b"
path = "/PYTHON/PRIVATE/"
`,
			wantErr: `extra_indexes[1].path "PYTHON/PRIVATE" duplicates extra_indexes[0].path`,
		},
		{
			name: "empty name",
			document: `
[[extra_indexes]]
name = " "
path = "python/private"
`,
			wantErr: "extra_indexes[0].name must not be empty",
		},
		{
			// Trimming would silently change the extra index cache identity.
			name: "name with surrounding whitespace",
			document: `
[[extra_indexes]]
name = " private "
path = "python/private"
`,
			wantErr: "must not contain leading or trailing whitespace",
		},
		{
			name: "oversized name",
			document: `
[[extra_indexes]]
name = ` + strconv.Quote(strings.Repeat("a", maxExtraIndexNameBytes+1)) + `
path = "python/private"
`,
			wantErr: "name must be at most 128 bytes",
		},
		{
			name: "duplicate names",
			document: `
[[extra_indexes]]
name = "Private"
path = "python/private-a"

[[extra_indexes]]
name = "private"
path = "python/private-b"
`,
			wantErr: `extra_indexes[1].name "private" duplicates extra_indexes[0].name`,
		},
		{
			name: "unknown kind",
			document: `
[[extra_indexes]]
name = "dynamic"
kind = "arbitrary"
path = "python/dynamic"
`,
			wantErr: `kind must be "pytorch" when set`,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := decodeConfigDocument([]byte(testCase.document))
			if err == nil {
				t.Fatalf("decodeConfigDocument accepted %s", testCase.name)
			}
			if !strings.Contains(err.Error(), testCase.wantErr) {
				t.Fatalf("error = %q, want %q", err, testCase.wantErr)
			}
		})
	}
}

func TestDecodeConfigDocumentRejectsUnsafeExtraIndexPath(t *testing.T) {
	for _, route := range []string{
		".",
		"..",
		"../admin",
		"python/./private",
		"python/../admin",
		"python//private",
		`python\private`,
		"python/:tenant",
		"python/*path",
		"python/%2e%2e",
		"python?tenant",
		"python#fragment",
		"私有源",
	} {
		t.Run(route, func(t *testing.T) {
			document := `
[[extra_indexes]]
name = "private"
path = ` + strconv.Quote(route) + "\n"
			_, err := decodeConfigDocument([]byte(document))
			if err == nil {
				t.Fatalf("decodeConfigDocument accepted unsafe path %q", route)
			}
			if !strings.Contains(err.Error(), "must contain only URL-safe literal path segments") {
				t.Fatalf("error = %q, want actionable route-syntax error", err)
			}
		})
	}
}

func TestDecodeConfigDocumentRejectsReservedExtraIndexRouteTrees(t *testing.T) {
	for route, want := range map[string]string{
		// A bare reserved segment names the offending field in full.
		"admin": `extra_indexes[0].path "admin" conflicts with reserved route "/admin"`,
		"pypi":  `extra_indexes[0].path "pypi" conflicts with reserved route "/pypi"`,
		// Route trees below a reserved segment are rejected as well.
		"admin/private": "/admin",
		"AdMiN/private": "/admin",
		"pypi/custom":   "/pypi",
		"v2/private":    "/v2",
		"api/private":   "/api",
		"mcp/private":   "/mcp",
		"health/probe":  "/health",
		"metrics/data":  "/metrics",
		"ccache/data":   "/ccache",
		"sccache/data":  "/sccache",
		"p/team":        "/p",
		"assets/custom": "/assets",
		"monitor/data":  "/monitor",
		"favicon.svg":   "/favicon.svg",
		"icons.svg/x":   "/icons.svg",
		"index.html":    "/index.html",
	} {
		t.Run(route, func(t *testing.T) {
			document := `
[[extra_indexes]]
name = "private"
path = ` + strconv.Quote(route) + "\n"
			_, err := decodeConfigDocument([]byte(document))
			if err == nil {
				t.Fatalf("decodeConfigDocument accepted reserved route tree %q", route)
			}
			if strings.HasPrefix(want, "/") {
				want = `conflicts with reserved route "` + want + `"`
			}
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("error = %q, want %q", err, want)
			}
		})
	}
}

func TestDecodeConfigDocumentAllowsFallbackOnlyMachineNamespaceForExtraIndex(t *testing.T) {
	for _, route := range []string{
		"cargo/custom",
		"docker/custom",
		"pip/custom",
		"gem/custom",
		"healthz/probe",
		"readyz/probe",
		"livez/probe",
		"metric/data",
		"metricsz/data",
		"apt-security",
	} {
		t.Run(route, func(t *testing.T) {
			document := `
[[extra_indexes]]
name = "private"
path = ` + strconv.Quote(route) + "\n"
			if _, err := decodeConfigDocument([]byte(document)); err != nil {
				t.Fatalf("decodeConfigDocument rejected available custom route %q: %v", route, err)
			}
		})
	}
}

func TestDecodeConfigDocumentAllowsMaximumLengthExtraIndexName(t *testing.T) {
	document := `
[[extra_indexes]]
name = ` + strconv.Quote(strings.Repeat("a", maxExtraIndexNameBytes)) + `
path = "python/private"
`
	if _, err := decodeConfigDocument([]byte(document)); err != nil {
		t.Fatalf("decodeConfigDocument rejected a maximum-length extra index name: %v", err)
	}
}

func TestDecodeConfigDocumentRejectsUnsafeExtraIndexName(t *testing.T) {
	for _, name := range []string{
		".",
		"..",
		"-private",
		"private-",
		"private/team",
		"private:name",
		"private*",
		"private mirror",
		"private%2fmirror",
		"私有源",
	} {
		t.Run(name, func(t *testing.T) {
			document := `
[[extra_indexes]]
name = ` + strconv.Quote(name) + `
path = "python/private"
`
			_, err := decodeConfigDocument([]byte(document))
			if err == nil {
				t.Fatalf("decodeConfigDocument accepted unsafe name %q", name)
			}
			if !strings.Contains(err.Error(), "must be a URL-safe slug") {
				t.Fatalf("error = %q, want actionable name error", err)
			}
		})
	}
}

func TestDecodeConfigDocumentProtectsPyTorchChannelNamespace(t *testing.T) {
	_, err := decodeConfigDocument([]byte(`
[[extra_indexes]]
name = "pytorch"
kind = "pytorch"
path = "pypi-torch"

[[extra_indexes]]
name = "shadow-channel"
path = "pypi-torch/cpu"
`))
	if err == nil || !strings.Contains(err.Error(), "overlaps the channel namespace") {
		t.Fatalf("channel namespace overlap error = %v", err)
	}
}

func TestDecodeConfigDocumentRejectsExtraIndexInsideAnotherProtocolSubtree(t *testing.T) {
	for _, testCase := range []struct {
		name      string
		firstPath string
		nextPath  string
	}{
		{name: "files child after parent", firstPath: "python", nextPath: "python/files/private"},
		{name: "files parent after child", firstPath: "python/files/private", nextPath: "python"},
		{name: "simple child after parent", firstPath: "python", nextPath: "python/simple/private"},
		{name: "case insensitive", firstPath: "Python", nextPath: "python/FILES/private"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			document := `
[[extra_indexes]]
name = "private-a"
path = ` + strconv.Quote(testCase.firstPath) + `

[[extra_indexes]]
name = "private-b"
path = ` + strconv.Quote(testCase.nextPath) + "\n"
			_, err := decodeConfigDocument([]byte(document))
			if err == nil {
				t.Fatal("decodeConfigDocument accepted an extra index inside another index's protocol subtree")
			}
			if !strings.Contains(err.Error(), `must not use another extra index's reserved "/simple" or "/files" subtree`) {
				t.Fatalf("error = %q, want actionable protocol-subtree error", err)
			}
		})
	}
}

func TestDecodeConfigDocumentNormalizesValidExtraIndexIdentities(t *testing.T) {
	cfg, err := decodeConfigDocument([]byte(`
[[extra_indexes]]
name = "python-root"
path = " /python/ "

[[extra_indexes]]
name = "pytorch-cu130"
path = " /python/private/ "

[[extra_indexes]]
name = "simple-boundary"
path = "python/simpleton"

[[extra_indexes]]
name = "reserved-prefix-boundary"
path = "apiary/private"
`))
	if err != nil {
		t.Fatalf("decodeConfigDocument rejected valid nested literal routes: %v", err)
	}
	if got, want := cfg.ExtraIndexes[0].Path, "python"; got != want {
		t.Fatalf("ExtraIndexes[0].Path = %q, want %q", got, want)
	}
	if got, want := cfg.ExtraIndexes[1].Path, "python/private"; got != want {
		t.Fatalf("ExtraIndexes[1].Path = %q, want %q", got, want)
	}
	if got, want := cfg.ExtraIndexes[2].Path, "python/simpleton"; got != want {
		t.Fatalf("ExtraIndexes[2].Path = %q, want %q", got, want)
	}
	if got, want := cfg.ExtraIndexes[3].Path, "apiary/private"; got != want {
		t.Fatalf("ExtraIndexes[3].Path = %q, want %q", got, want)
	}
}

func TestDecodeConfigDocumentEnablesPyTorchPresetByDefault(t *testing.T) {
	cfg, err := decodeConfigDocument(nil)
	if err != nil {
		t.Fatalf("decodeConfigDocument: %v", err)
	}
	if len(cfg.ExtraIndexes) != 1 {
		t.Fatalf("len(ExtraIndexes) = %d, want 1", len(cfg.ExtraIndexes))
	}

	preset := cfg.ExtraIndexes[0]
	if preset.Name != "pytorch" || preset.Kind != ExtraIndexKindPyTorch || preset.Path != "pypi-torch" || preset.SimplePath != "/" {
		t.Fatalf("preset identity = %#v, want channel-aware pytorch at pypi-torch with root simple path", preset)
	}
	if len(preset.Upstreams) != 1 {
		t.Fatalf("len(preset.Upstreams) = %d, want 1", len(preset.Upstreams))
	}
	upstream := preset.Upstreams[0]
	if upstream.Name != "pytorch" || upstream.URL != "https://download.pytorch.org/whl" || upstream.Priority != 1 || upstream.ProbeMode != "passive" {
		t.Fatalf("preset upstream = %#v, want passive PyTorch wheel-family upstream", upstream)
	}
}

func TestDecodeConfigDocumentPreservesOperatorOrderAndAppendsMissingPresets(t *testing.T) {
	cfg, err := decodeConfigDocument([]byte(`
[[extra_indexes]]
name = "private-first"
path = "python/private"

[[extra_indexes.upstreams]]
name = "private"
url = "https://packages.example"
priority = 1
`))
	if err != nil {
		t.Fatalf("decodeConfigDocument: %v", err)
	}
	if len(cfg.ExtraIndexes) != 2 {
		t.Fatalf("len(ExtraIndexes) = %d, want operator index plus preset", len(cfg.ExtraIndexes))
	}
	if cfg.ExtraIndexes[0].Name != "private-first" || cfg.ExtraIndexes[1].Name != "pytorch" {
		t.Fatalf("ExtraIndexes order = [%q, %q], want operator order followed by preset", cfg.ExtraIndexes[0].Name, cfg.ExtraIndexes[1].Name)
	}
	if got, want := cfg.ExtraIndexes[0].SimplePath, "/simple"; got != want {
		t.Fatalf("operator default SimplePath = %q, want %q", got, want)
	}
}

func TestDecodeConfigDocumentExplicitNameOverridesBuiltinPresetInPlace(t *testing.T) {
	cfg, err := decodeConfigDocument([]byte(`
[[extra_indexes]]
name = "private-first"
path = "python/private"

[[extra_indexes]]
name = "PyTorch"
path = "company/pytorch"
simple_path = "/root/"

[[extra_indexes.upstreams]]
name = "company"
url = "https://packages.example/pytorch"
priority = 1
probe_mode = "passive"
`))
	if err != nil {
		t.Fatalf("decodeConfigDocument: %v", err)
	}
	if len(cfg.ExtraIndexes) != 2 {
		t.Fatalf("len(ExtraIndexes) = %d, want only the two operator indexes", len(cfg.ExtraIndexes))
	}
	if cfg.ExtraIndexes[0].Name != "private-first" || cfg.ExtraIndexes[1].Name != "PyTorch" {
		t.Fatalf("operator order was not preserved: %#v", cfg.ExtraIndexes)
	}
	if got, want := cfg.ExtraIndexes[1].Path, "company/pytorch"; got != want {
		t.Fatalf("explicit preset override Path = %q, want %q", got, want)
	}
	if got, want := cfg.ExtraIndexes[1].SimplePath, "/root"; got != want {
		t.Fatalf("explicit preset override SimplePath = %q, want %q", got, want)
	}
}

func TestDecodeConfigDocumentExplicitPresetNameInheritsMissingLayoutFields(t *testing.T) {
	cfg, err := decodeConfigDocument([]byte(`
[[extra_indexes]]
name = "pytorch"

[[extra_indexes.upstreams]]
name = "company-cache"
url = "https://packages.example/pytorch/cu128"
priority = 1
probe_mode = "passive"
`))
	if err != nil {
		t.Fatalf("decodeConfigDocument: %v", err)
	}
	if len(cfg.ExtraIndexes) != 1 {
		t.Fatalf("len(ExtraIndexes) = %d, want one overlaid preset", len(cfg.ExtraIndexes))
	}
	index := cfg.ExtraIndexes[0]
	if index.Kind != ExtraIndexKindPyTorch || index.Path != "pypi-torch" || index.SimplePath != "/" {
		t.Fatalf("preset layout = path %q, simple_path %q", index.Path, index.SimplePath)
	}
	if len(index.Upstreams) != 1 || index.Upstreams[0].Name != "company-cache" {
		t.Fatalf("operator upstream was not preserved: %#v", index.Upstreams)
	}
}

func TestDecodeConfigDocumentCanonicalPresetPathDoesNotDuplicateOlderManualRoute(t *testing.T) {
	cfg, err := decodeConfigDocument([]byte(`
[[extra_indexes]]
name = "torch-wheels"
path = "/PYPI-TORCH/"

[[extra_indexes.upstreams]]
name = "pytorch"
url = "https://download.pytorch.org/whl"
priority = 1
probe_mode = "passive"
`))
	if err != nil {
		t.Fatalf("decodeConfigDocument: %v", err)
	}
	if len(cfg.ExtraIndexes) != 1 {
		t.Fatalf("ExtraIndexes = %#v, want the operator-owned canonical route only", cfg.ExtraIndexes)
	}
	if cfg.ExtraIndexes[0].Name != "torch-wheels" || cfg.ExtraIndexes[0].Path != "PYPI-TORCH" {
		t.Fatalf("operator route was not preserved: %#v", cfg.ExtraIndexes[0])
	}
	if cfg.ExtraIndexes[0].SimplePath != "/" {
		t.Fatalf("older PyTorch route SimplePath = %q, want inherited root layout", cfg.ExtraIndexes[0].SimplePath)
	}
}

func TestDecodeConfigDocumentRepurposedCanonicalPathKeepsOperatorLayout(t *testing.T) {
	cfg, err := decodeConfigDocument([]byte(`
[[extra_indexes]]
name = "company-wheels"
path = "pypi-torch"

[[extra_indexes.upstreams]]
name = "company"
url = "https://packages.example/python"
priority = 1
probe_mode = "passive"
`))
	if err != nil {
		t.Fatalf("decodeConfigDocument: %v", err)
	}
	if len(cfg.ExtraIndexes) != 1 || cfg.ExtraIndexes[0].SimplePath != "/simple" {
		t.Fatalf("repurposed route = %#v, want operator route with normal /simple layout", cfg.ExtraIndexes)
	}
}

func TestDecodeConfigDocumentCanDisableExtraIndexPresets(t *testing.T) {
	for _, testCase := range []struct {
		name     string
		document string
	}{
		{
			name: "all",
			document: `
[extra_index_presets]
enabled = false
`,
		},
		{
			name: "one",
			document: `
[extra_index_presets]
disabled = ["pytorch"]
`,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			cfg, err := decodeConfigDocument([]byte(testCase.document))
			if err != nil {
				t.Fatalf("decodeConfigDocument: %v", err)
			}
			if len(cfg.ExtraIndexes) != 0 {
				t.Fatalf("ExtraIndexes = %#v, want no built-in presets", cfg.ExtraIndexes)
			}
		})
	}
}

func TestDecodeConfigDocumentRejectsUnknownDisabledPreset(t *testing.T) {
	_, err := decodeConfigDocument([]byte(`
[extra_index_presets]
enabled = false
disabled = ["pytorch-cu999"]
`))
	if err == nil {
		t.Fatal("decodeConfigDocument accepted an unknown disabled preset")
	}
	if !strings.Contains(err.Error(), `names unknown preset "pytorch-cu999"`) {
		t.Fatalf("error = %q, want actionable unknown-preset error", err)
	}
}
