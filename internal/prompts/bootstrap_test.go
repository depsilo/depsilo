package prompts_test

import (
	"strings"
	"testing"

	"depsilo/internal/ecosystem"
	"depsilo/internal/prompts"
)

const bootstrapBase = "http://depsilo.example"

// allBootstrapEcosystems are the adapter names /api/v1/discover can report.
var allBootstrapEcosystems = []string{
	"pypi", "apt", "npm", "go", "cargo", "maven", "rubygems", "composer",
	"nuget", "conda", "cran", "alpine", "helm", "huggingface", "docker",
}

// The prompt is written into project files every collaborator's agent reads,
// so the rule about asking first has to come before any setting.
func TestBootstrap_OpensWithTheAskFirstRule(t *testing.T) {
	out := prompts.Bootstrap(bootstrapBase, allBootstrapEcosystems, nil)
	rule := strings.Index(out, "prefer a setting that lives in this project or\napplies to a single command")
	approval := strings.Index(out, "show the user the exact\nchanges and wait for approval")
	firstSetting := strings.Index(out, "pip config set")
	if rule == -1 || approval == -1 || firstSetting == -1 {
		t.Fatalf("rule = %d, approval = %d, first setting = %d\n%s", rule, approval, firstSetting, out)
	}
	if rule > firstSetting || approval > firstSetting {
		t.Fatalf("the rule must come before the settings:\n%s", out)
	}
	if strings.Contains(out, "Before running any package install") {
		t.Errorf("the prompt still tells agents to configure before every install")
	}
}

// Every setting that outlives the project or the command is labelled, so the
// rule has something to bind to.
func TestBootstrap_LabelsUserAndSystemLevelSettings(t *testing.T) {
	out := prompts.Bootstrap(bootstrapBase, allBootstrapEcosystems, nil)
	_, settings, found := strings.Cut(out, "This instance serves:")
	if !found {
		t.Fatalf("no ecosystem list:\n%s", out)
	}
	settings, _, _ = strings.Cut(settings, "npm lockfiles:")
	for _, line := range strings.Split(settings, "\n") {
		// "bundle config set --local" writes into the project.
		if strings.Contains(line, " --local ") {
			if !strings.Contains(line, "project:") {
				t.Errorf("project setting is not labelled: %q", line)
			}
			continue
		}
		for _, marker := range []string{"~/", "config set", "config --set", "env -w", " -g ", "repo add", "add source", "bundle config"} {
			if strings.Contains(line, marker) && !strings.Contains(line, "user-level") {
				t.Errorf("user-level setting is not labelled: %q", line)
			}
		}
		if strings.Contains(line, "/etc/") && !strings.Contains(line, "system-level") {
			t.Errorf("system-level setting is not labelled: %q", line)
		}
	}
}

func TestBootstrap_ListsOnlyTheServedEcosystems(t *testing.T) {
	out := prompts.Bootstrap(bootstrapBase, []string{"npm", "pypi", "custom"}, nil)
	for _, expected := range []string{
		"PIP_INDEX_URL=http://depsilo.example/pypi/simple/",
		"registry=http://depsilo.example/npm/",
	} {
		if !strings.Contains(out, expected) {
			t.Errorf("prompt omits %q", expected)
		}
	}
	for _, absent := range []string{
		"GOPROXY", "/crates/", "/maven/", "/rubygems/", "/composer/", "/nuget/",
		"/conda/", "/helm/", "HF_ENDPOINT", "/cran/", "/apt", "/alpine/",
		"registry-mirrors", "custom", "Add ?ecosystem=",
	} {
		if strings.Contains(out, absent) {
			t.Errorf("prompt names %q, which this instance does not serve", absent)
		}
	}
}

// pip ignores an http:// index on a non-loopback host unless the host is
// trusted, so on a plain-HTTP instance the pip entry has to carry that too.
func TestBootstrap_PipTrustsThePlainHTTPHost(t *testing.T) {
	out := prompts.Bootstrap("http://depsilo.example:23333", []string{"pypi"}, nil)
	for _, expected := range []string{
		"PIP_TRUSTED_HOST=depsilo.example:23333",
		"user-level:   pip config set global.trusted-host depsilo.example:23333",
	} {
		if !strings.Contains(out, expected) {
			t.Errorf("plain-HTTP prompt omits %q:\n%s", expected, out)
		}
	}
	if strings.Contains(out, "{{") {
		t.Errorf("unrendered placeholder:\n%s", out)
	}

	// HTTPS needs no trusted host, and the note belongs to pip alone.
	for name, other := range map[string]string{
		"https":   prompts.Bootstrap("https://depsilo.example", []string{"pypi"}, nil),
		"no pypi": prompts.Bootstrap("http://depsilo.example:23333", []string{"npm"}, nil),
	} {
		if strings.Contains(other, "TRUSTED_HOST") || strings.Contains(other, "trusted-host") {
			t.Errorf("%s prompt talks about a trusted host:\n%s", name, other)
		}
	}
}

// .NET SDK 9 refuses an http source (NU1302) and Composer an http repository
// unless they opt in, so on a plain-HTTP instance their entries carry the
// Portal's opt-ins; HTTPS needs neither.
func TestBootstrap_NuGetAndComposerOptInToPlainHTTP(t *testing.T) {
	served := []string{"nuget", "composer"}
	out := prompts.Bootstrap("http://depsilo.example:23333", served, nil)
	for _, expected := range []string{
		"user-level:   composer config -g secure-http false",
		"refuses this source (NU1302), and the\n                          command line cannot opt in: append\n                          --allow-insecure-connections to the user-level line\n",
		"(SDK 8 and earlier reject that flag)",
	} {
		if !strings.Contains(out, expected) {
			t.Errorf("plain-HTTP prompt omits %q:\n%s", expected, out)
		}
	}
	https := prompts.Bootstrap("https://depsilo.example", served, nil)
	for _, absent := range []string{"secure-http", "insecure"} {
		if strings.Contains(https, absent) {
			t.Errorf("HTTPS prompt names %q:\n%s", absent, https)
		}
	}
}

// The settings in bootstrap.go are written by hand. An ecosystem the catalog
// gains without an entry there would be reported by /api/v1/discover and left
// out of the prompt, and an instance serving only that one would be told it
// serves nothing. Docker is served outside the standard upstreams, so it is
// named here whatever the catalog lists.
func TestBootstrap_HasAnEntryForEveryCatalogEcosystem(t *testing.T) {
	routes := map[string]string{"docker": ""}
	for _, definition := range ecosystem.All() {
		if definition.Name != "docker" {
			routes[definition.Name] = definition.Route
		}
	}
	for name, route := range routes {
		out := prompts.Bootstrap(bootstrapBase, []string{name}, nil)
		if !strings.Contains(out, "This instance serves:") || strings.Contains(out, "reports no package ecosystems") {
			t.Errorf("the bootstrap prompt has no entry for %s", name)
			continue
		}
		// The entry points at the ecosystem's route; Docker uses the root.
		_, entry, _ := strings.Cut(out, "This instance serves:")
		entry, _, _ = strings.Cut(entry, "Dockerfiles and CI files have their own guide")
		if !strings.Contains(entry, bootstrapBase+route) {
			t.Errorf("the %s entry does not name %s:\n%s", name, bootstrapBase+route, entry)
		}
	}
}

func TestBootstrap_SaysSoWhenNothingIsServed(t *testing.T) {
	out := prompts.Bootstrap(bootstrapBase, nil, nil)
	if !strings.Contains(out, "reports no package ecosystems") || strings.Contains(out, "pip") {
		t.Fatalf("empty instance prompt:\n%s", out)
	}
}

// The Portal is a JavaScript page; an agent with curl gets an empty shell
// from it. Cargo and Maven settings are spelled out instead.
func TestBootstrap_GivesCargoAndMavenInline(t *testing.T) {
	out := prompts.Bootstrap(bootstrapBase, []string{"cargo", "maven"}, nil)
	for _, expected := range []string{
		".cargo/config.toml",
		"[source.crates-io]",
		`replace-with = "depsilo"`,
		"[source.depsilo]",
		`registry = "sparse+http://depsilo.example/crates/"`,
		"~/.m2/settings.xml",
		"<id>depsilo</id>",
		"<mirrorOf>*</mirrorOf>",
		"<url>http://depsilo.example/maven/</url>",
	} {
		if !strings.Contains(out, expected) {
			t.Errorf("prompt omits %q", expected)
		}
	}
	if strings.Contains(out, "visit ") || strings.Contains(out, "copy the") {
		t.Errorf("prompt still sends the agent to the Portal page:\n%s", out)
	}
}

func TestBootstrap_CoversAptAlpineAndDocker(t *testing.T) {
	out := prompts.Bootstrap(bootstrapBase, []string{"apt", "alpine", "docker"}, nil)
	for _, expected := range []string{
		"/etc/apt/sources.list: replace the host with http://depsilo.example/apt",
		"http://depsilo.example/alpine/v<major.minor>/main",
		"http://depsilo.example/alpine/v<major.minor>/community",
		`{"registry-mirrors": ["http://depsilo.example"]}`,
		"show it to the operator, do not edit it",
		"http://depsilo.example/api/v1/integration-prompt",
		"debian (apt), alpine (apk), container (Docker)",
	} {
		if !strings.Contains(out, expected) {
			t.Errorf("prompt omits %q", expected)
		}
	}
	// The pointer has to name scopes the integration guide accepts.
	for _, id := range []string{"debian", "alpine", "container"} {
		if _, ok := prompts.IntegrationFor(bootstrapBase, id); !ok {
			t.Errorf("integration guide has no %q scope", id)
		}
	}
}

// "bundle config NAME VALUE" without a scope flag writes ~/.bundle/config
// with Bundler 1.x and the project's .bundle/config with Bundler 2.1 and
// later inside a project, so every Bundler command names its scope.
func TestBootstrap_BundlerCommandsNameTheirScope(t *testing.T) {
	out := prompts.Bootstrap(bootstrapBase, []string{"rubygems"}, nil)
	for _, expected := range []string{
		"project:      bundle config set --local mirror.https://rubygems.org http://depsilo.example/rubygems/\n",
		"user-level:   bundle config set --global mirror.https://rubygems.org http://depsilo.example/rubygems/\n",
		`(check "bundle --version" first: before 2.1, leave out "set")`,
		"gem install --source http://depsilo.example/rubygems/",
	} {
		if !strings.Contains(out, expected) {
			t.Errorf("prompt omits %q:\n%s", expected, out)
		}
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "bundle config") && !strings.Contains(line, " --local ") && !strings.Contains(line, " --global ") {
			t.Errorf("Bundler command without a scope: %q", line)
		}
	}
}

// Depsilo's npm route serves only the signed tarball URLs of its rewritten
// metadata, so a lockfile recorded against another registry fails with 404.
func TestBootstrap_ExplainsNpmLockfiles(t *testing.T) {
	out := prompts.Bootstrap(bootstrapBase, []string{"npm"}, nil)
	for _, expected := range []string{
		"omit-lockfile-registry-resolved=true in its .npmrc",
		`"npm install --package-lock-only --omit-lockfile-registry-resolved"`,
		"404",
		`"npm ci" alone does not help`,
		"ask before rewriting it",
	} {
		if !strings.Contains(out, expected) {
			t.Errorf("npm guidance omits %q:\n%s", expected, out)
		}
	}
}

// pnpm, Yarn, Bun and the Python clients other than pip read settings of
// their own, and the Node lockfiles other than npm's have no rewrite flag,
// so the npm and pip entries name them and point at the integration guide.
func TestBootstrap_PointsOtherNodeAndPythonClientsAtTheGuide(t *testing.T) {
	out := prompts.Bootstrap(bootstrapBase, []string{"npm", "pypi"}, nil)
	for _, expected := range []string{
		"pnpm, Yarn, Bun: see http://depsilo.example/api/v1/integration-prompt?ecosystem=node\n",
		"uv, Poetry, PDM, Pipenv: see http://depsilo.example/api/v1/integration-prompt?ecosystem=python\n",
		"pnpm, Yarn 2+ and Bun lockfiles can fail the same way and have no such\n   flag: report them; do not regenerate one without approval.",
	} {
		if !strings.Contains(out, expected) {
			t.Errorf("prompt omits %q:\n%s", expected, out)
		}
	}
	for _, id := range []string{"node", "python"} {
		if _, ok := prompts.IntegrationFor(bootstrapBase, id); !ok {
			t.Errorf("integration guide has no %q scope", id)
		}
	}
}

func TestBootstrap_GoFailoverWarningFollowsTheGoEcosystem(t *testing.T) {
	const warning = `do not use GOPROXY "|direct"`
	if out := prompts.Bootstrap(bootstrapBase, []string{"go"}, nil); !strings.Contains(out, warning) || !strings.Contains(out, "do not provide reliable outage failover") {
		t.Errorf("Go instance lost the failover warning:\n%s", out)
	}
	if out := prompts.Bootstrap(bootstrapBase, []string{"npm"}, nil); strings.Contains(out, "GOPROXY") || !strings.Contains(out, "do not provide reliable outage failover") {
		t.Errorf("npm-only instance prompt:\n%s", out)
	}
	// Likewise the lockfile note belongs to npm.
	if out := prompts.Bootstrap(bootstrapBase, []string{"go"}, nil); strings.Contains(out, "lockfile") {
		t.Errorf("Go-only instance prompt talks about npm lockfiles:\n%s", out)
	}
}

func TestBootstrap_NamesTheMCPToolsItIsGiven(t *testing.T) {
	out := prompts.Bootstrap(bootstrapBase, nil, []string{"depsilo_status", "depsilo_doctor"})
	for _, expected := range []string{
		"http://depsilo.example/mcp",
		"Authorization: Bearer",
		"read-only API token",
		"Available tools:\ndepsilo_status, depsilo_doctor.",
	} {
		if !strings.Contains(out, expected) {
			t.Errorf("prompt omits %q:\n%s", expected, out)
		}
	}
}

// init-agent falls back to this text when it cannot reach the instance. It is
// the same prompt for every ecosystem, and it says that it is not specific.
func TestBootstrapOffline_ListsEveryEcosystemAndSaysSo(t *testing.T) {
	out := prompts.BootstrapOffline(bootstrapBase + "/")
	for _, expected := range []string{
		"written\n   without reaching the instance",
		"http://depsilo.example/api/v1/discover names the ones this instance serves",
		"http://depsilo.example/mcp",
		"Authorization: Bearer",
		"read-only API token",
	} {
		if !strings.Contains(out, expected) {
			t.Errorf("offline prompt omits %q:\n%s", expected, out)
		}
	}
	if strings.Contains(out, "http://depsilo.example//") || strings.Contains(out, "{{MIRROR_URL}}") {
		t.Errorf("offline prompt renders the base badly:\n%s", out)
	}

	// Apart from the sentence about its origin and the MCP tool names, it is
	// the text an instance serving everything would send.
	online := prompts.Bootstrap(bootstrapBase, allBootstrapEcosystems, nil)
	_, offlineSettings, _ := strings.Cut(out, "\n\n   pip ")
	_, onlineSettings, found := strings.Cut(online, "\n\n   pip ")
	if !found || offlineSettings != onlineSettings {
		t.Errorf("offline settings differ from the served ones:\noffline:\n%s\nonline:\n%s", offlineSettings, onlineSettings)
	}
}
