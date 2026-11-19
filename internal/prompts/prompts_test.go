package prompts_test

import (
	"regexp"
	"strings"
	"testing"

	"depsilo/internal/prompts"
)

// The mirror URL replaces every placeholder, without the trailing slash it
// was given: the GOPROXY line below would otherwise read ".../23333//go".
func TestIntegration_RenderedPromptIsTransparent(t *testing.T) {
	out := prompts.Integration("http://depsilo.example:23333/")
	if strings.Contains(out, "{{MIRROR_URL}}") {
		t.Errorf("placeholder still present after rendering")
	}
	mustContain := []string{
		"You are integrating **Depsilo**",
		"Routed via Depsilo cache: http://depsilo.example:23333",
		"Public-registry fallback",
		"Do not set `PIP_EXTRA_INDEX_URL`",
		"GOPROXY=\"http://depsilo.example:23333/go,direct\"",
	}
	for _, s := range mustContain {
		if !strings.Contains(out, s) {
			t.Errorf("rendered transparent prompt missing %q", s)
		}
	}
}

// The template is handed to coding agents as it is. Its example for the
// mirror URL is the placeholder origin, and no line carries a private address
// from somebody's network.
func TestIntegrationTemplate_ExampleAddressIsThePlaceholderOrigin(t *testing.T) {
	tmpl := prompts.IntegrationTemplate()
	if !strings.Contains(tmpl, "`{{MIRROR_URL}}` → the running service's public URL (e.g. `http://depsilo.example:23333`)") {
		t.Error("the {{MIRROR_URL}} line does not give http://depsilo.example:23333 as its example")
	}
	privateAddress := regexp.MustCompile(`\b(10\.\d+\.\d+\.\d+|192\.168\.\d+\.\d+|172\.(1[6-9]|2\d|3[01])\.\d+\.\d+)\b`)
	if found := privateAddress.FindString(tmpl); found != "" {
		t.Errorf("the template names the private address %s", found)
	}
}

func TestIntegrationTemplate_HasMandatorySections(t *testing.T) {
	tmpl := prompts.IntegrationTemplate()
	// Sanity guards: if any of these phrases get accidentally removed during
	// future edits, the prompt loses load-bearing safety constraints.
	mustContain := []string{
		// Transparency-by-default discipline. "Identification" replaced the
		// previous "Brand discipline" (stealth) section in T0 #1 — see
		// docs/adr/0003-supply-chain-control-point.md. If a future edit
		// reintroduces brand-neutrality / stealth language this assertion
		// must fail loudly.
		"Identification",
		"Depsilo",
		// Public-registry fallback is non-negotiable — a self-hosted
		// enforcement layer that brings the build down when it's offline is
		// worse than no recovery path.
		"Public-registry fallback",
		"Discover before editing",
		"Hard constraints",
		"Never",
		"HTTP_PROXY",
		"lockfile",
	}
	for _, s := range mustContain {
		if !strings.Contains(tmpl, s) {
			t.Errorf("integration prompt missing section/phrase %q", s)
		}
	}
}

// TestIntegrationTemplate_NoStealthLanguage guards against accidental
// reintroduction of the brand-neutral / stealth framing the prompt used to
// have. A security-positioned tool must not instruct LLMs to hide what it
// changed, so any of these phrases creeping back in is a regression.
func TestIntegrationTemplate_NoStealthLanguage(t *testing.T) {
	tmpl := prompts.IntegrationTemplate()
	mustNotContain := []string{
		"Brand discipline",
		"brand-neutral",
		"no longer depends on direct public CDN",
		"opaque internal address",
		"Do not write the mirror's product name",
		"never writes the product name",
	}
	for _, s := range mustNotContain {
		if strings.Contains(tmpl, s) {
			t.Errorf("stealth-language regression: prompt contains forbidden phrase %q", s)
		}
	}
}

// TestIntegration_StripsMaintainerHeader asserts that the doc-comment block
// above the `---` separator (intended for repo maintainers) does NOT leak into
// the rendered LLM-facing payload.
func TestIntegration_StripsMaintainerHeader(t *testing.T) {
	out := prompts.Integration("http://example.local")
	for _, phrase := range []string{
		"This file is the canonical prompt",
		"Placeholders are",
		"Edits here ship to every user",
	} {
		if strings.Contains(out, phrase) {
			t.Errorf("maintainer doc-comment leaked into rendered prompt: contains %q", phrase)
		}
	}
	// Real content from below the separator should still be present.
	if !strings.Contains(out, "You are integrating **Depsilo**") {
		t.Errorf("rendered prompt missing opening instruction line")
	}
}

// The README promises that the agent presents its plan first. Every way of
// getting the guide (the endpoint, "depsilo prompt", the Portal's full text)
// renders this template, so the step has to be in it.
func TestIntegration_PresentsAPlanAndWaitsBeforeEditing(t *testing.T) {
	out := prompts.Integration("http://example.local")
	_, work, found := strings.Cut(out, "## How to work")
	if !found {
		t.Fatal("no \"How to work\" section")
	}
	work, _, _ = strings.Cut(work, "\n## ")
	work = strings.Join(strings.Fields(work), " ")

	discover := strings.Index(work, "1. **Discover before editing.**")
	plan := strings.Index(work, "2. **Present a plan and wait.**")
	if discover == -1 || plan == -1 || plan < discover {
		t.Fatalf("the plan step must follow discovery: discover = %d, plan = %d", discover, plan)
	}
	for _, expected := range []string{
		"before changing any file",
		"list every file you intend to change and the edit for each",
		"stop and wait for the user to confirm",
		"If nobody can confirm (a non-interactive run), stop after presenting the plan",
	} {
		if !strings.Contains(work, expected) {
			t.Errorf("plan step omits %q", expected)
		}
	}
}

// Yarn, Bun, Poetry and PDM get only a checked-in project file in the table,
// while this step prefers build-time injection and the table forbids other
// names. The step has to say which wins, and the rule against published
// files still holds for a library's pyproject.toml.
func TestIntegration_SaysWhatToDoWhenTheTableGivesOnlyAProjectFile(t *testing.T) {
	out := prompts.Integration("http://example.local")
	_, step, found := strings.Cut(out, "4. **Inject at build time, not in checked-in dev configs.**")
	if !found {
		t.Fatal("no build-time injection step")
	}
	step, _, _ = strings.Cut(step, "\n5. ")
	step = strings.Join(strings.Fields(step), " ")
	const expected = "Where the table below gives a client only a project file, use that file and list it in the plan, unless the project publishes it (a library's `pyproject.toml`): then leave it and report."
	if !strings.Contains(step, expected) {
		t.Errorf("the build-time injection step omits %q:\n%s", expected, step)
	}
}

// Depsilo's npm route serves only the signed tarball URLs of its rewritten
// metadata, so "the mirror serves the same artifacts, never touch lockfiles"
// was wrong for Node projects.
func TestIntegration_LockfileGuidanceMatchesTheNpmRoute(t *testing.T) {
	out := strings.Join(strings.Fields(prompts.Integration("http://example.local")), " ")
	for _, expected := range []string{
		"**Never** modify lockfiles, with one exception that the user approves",
		"return 404",
		"`package-lock.json` with `resolved` URLs of any registry (`registry.npmjs.org`: 404; any other: bypass), propose rewriting it",
		"`npm install --package-lock-only --omit-lockfile-registry-resolved`",
		"keeps every `integrity` value",
		"Yarn 1, and Bun with another registry's URLs, do not fail: they follow the recorded URLs and bypass Depsilo.",
		"installs both through Depsilo and directly from the public registry",
		"Passing the flag to `npm ci` or `npm install` alone",
		"`omit-lockfile-registry-resolved=true` in the project `.npmrc`",
		"Do not edit or regenerate `pnpm-lock.yaml`, `yarn.lock`, `bun.lock` or `bun.lockb`",
		"tied to this instance",
		"Report the limitation to the user",
		// Pointing a locking Python client at another index invalidates its
		// lockfile, and uv re-locks without being asked.
		"An index set in `pyproject.toml` or `Pipfile`, or through `UV_INDEX_URL`, makes `uv.lock`, `poetry.lock`, `pdm.lock` or `Pipfile.lock` stale",
		"a re-lock resolves it against this instance (a plain `uv sync` re-locks unasked)",
		"Do not re-lock: name the stale lockfile in the plan and let the user decide",
		"Every other lockfile stays untouched",
	} {
		if !strings.Contains(out, expected) {
			t.Errorf("lockfile guidance omits %q", expected)
		}
	}
	for _, wrong := range []string{
		"The mirror serves the same artifacts",
		"integrity hashes still match",
	} {
		if strings.Contains(out, wrong) {
			t.Errorf("lockfile guidance still claims %q", wrong)
		}
	}
}

// knobRow returns the per-ecosystem table row whose first cell is client.
func knobRow(t *testing.T, prompt, client string) string {
	t.Helper()
	for _, line := range strings.Split(prompt, "\n") {
		if cells := strings.Split(line, "|"); len(cells) > 3 && strings.TrimSpace(cells[1]) == client {
			return line
		}
	}
	t.Fatalf("the per-ecosystem table has no row for %s", client)
	return ""
}

// The agent is told to use the table's names verbatim, so a row shared by
// several clients hands some of them a setting they do not read: pnpm 11
// ignores the npm variable, Yarn and Bun read their own files, and Poetry,
// Gradle and sbt read neither pip's variable nor Maven's settings.xml. The
// settings are the ones in web/src/lib/ecosystemData.ts.
func TestIntegration_GivesEachClientTheSettingItReads(t *testing.T) {
	out := prompts.Integration("http://example.local")
	if !strings.Contains(strings.Join(strings.Fields(out), " "), "use those names verbatim; do not invent new ones or carry one client's setting over to another") {
		t.Errorf("the table no longer binds the agent to the names it gives")
	}
	for _, row := range []struct {
		client string
		reads  []string
		not    []string
	}{
		{"npm", []string{"`registry=<URL>/npm/` in the project `.npmrc`", "env `npm_config_registry`"}, nil},
		{"pnpm", []string{
			"`registry=<URL>/npm/` in the project `.npmrc` (every version)",
			"`PNPM_CONFIG_REGISTRY` for pnpm 11 and later (they ignore `npm_config_registry`)",
			"`npm_config_registry` for pnpm 10 and earlier",
		}, nil},
		{"Yarn 2+", []string{"`npmRegistryServer: \"<URL>/npm/\"` in `.yarnrc.yml`", "for plain HTTP also list the host name, without the port, under `unsafeHttpWhitelist:`"}, []string{"npm_config_registry", ".npmrc"}},
		{"Yarn 1", []string{"`registry \"<URL>/npm/\"` in `.yarnrc`"}, []string{"npm_config_registry", ".npmrc", "npmRegistryServer"}},
		// No exclusions: Bun also reads the npm variable and .npmrc.
		{"Bun", []string{"`[install] registry = \"<URL>/npm/\"` in `bunfig.toml`"}, nil},
		{"pip", []string{"env `PIP_INDEX_URL`", "`PIP_TRUSTED_HOST=<host>` only for plain HTTP"}, nil},
		{"uv", []string{"env `UV_INDEX_URL`", "`[[tool.uv.index]]`", "`default = true` in `pyproject.toml`"}, []string{"PIP_"}},
		{"Poetry", []string{"`[[tool.poetry.source]]`", "`priority = \"primary\"` in `pyproject.toml`"}, []string{"PIP_"}},
		{"PDM", []string{"`[[tool.pdm.source]]` with `name = \"pypi\"`", "`verify_ssl = false` only for plain HTTP"}, []string{"PIP_"}},
		{"Pipenv", []string{"env `PIPENV_PYPI_MIRROR`", "`[[source]]`", "in `Pipfile`", "`verify_ssl = false` only for plain HTTP"}, []string{"PIP_"}},
		{"Maven", []string{"`~/.m2/settings.xml`", "<id>depsilo</id><mirrorOf>*</mirrorOf>"}, nil},
		{"Gradle", []string{"`maven { url \"<URL>/maven/\" }`", "`~/.gradle/init.gradle`", "for plain HTTP also `allowInsecureProtocol = true`"}, []string{"settings.xml"}},
		{"sbt", []string{"`~/.sbt/repositories`", "`depsilo: <URL>/maven/`", "`-Dsbt.override.build.repos=true`"}, []string{"settings.xml"}},
		{"RubyGems (`gem`)", []string{"`gem install --source <URL>/rubygems/`"}, []string{"bundle"}},
		{"Bundler", []string{
			"`bundle config set --local mirror.https://rubygems.org <URL>/rubygems/` writes the project's `.bundle/config`",
			"`--global` writes `~/.bundle/config`",
			"check `bundle --version` first: before 2.1 leave out `set`",
		}, nil},
		// <URL> is the service origin in every row, so the path is spelled out.
		{"PHP / Composer", []string{"`composer config -g repo.packagist composer <URL>/composer/`", "for plain HTTP also `composer config -g secure-http false`"}, nil},
		{".NET / NuGet", []string{"`<packageSources>` entry named `depsilo`", "for plain HTTP also `allowInsecureConnections=\"true\"` on it"}, nil},
		{"R / CRAN", []string{"`options(repos = c(CRAN = \"<URL>/cran/\"))`"}, nil},
		{"Helm", []string{"`helm repo add depsilo <URL>/helm/`"}, nil},
	} {
		line := knobRow(t, out, row.client)
		for _, expected := range row.reads {
			if !strings.Contains(line, expected) {
				t.Errorf("%s row omits %q:\n%s", row.client, expected, line)
			}
		}
		for _, wrong := range row.not {
			if strings.Contains(line, wrong) {
				t.Errorf("%s row names %q, which is another client's setting:\n%s", row.client, wrong, line)
			}
		}
	}
}

// The Go route relays checksum database requests (internal/adapter/goproxy),
// so the rule against GOSUMDB=off costs a Go project nothing.
func TestIntegration_SaysGoChecksumVerificationNeedsNoChange(t *testing.T) {
	out := strings.Join(strings.Fields(prompts.Integration("http://example.local")), " ")
	_, constraints, found := strings.Cut(out, "## Hard constraints")
	if !found {
		t.Fatal("no \"Hard constraints\" section")
	}
	for _, expected := range []string{
		"`GOSUMDB=off` for projects that don't already set it",
		"Go checksum verification stays on and `GOSUMDB` needs no change: Depsilo relays checksum-database requests when a Go Upstream mirrors the database, and otherwise the go command asks sum.golang.org itself",
		"only where that host is unreachable, propose `GOSUMDB=sum.golang.google.cn`",
	} {
		if !strings.Contains(constraints, expected) {
			t.Errorf("hard constraints omit %q", expected)
		}
	}
}

func TestIntegrationFor_EmptyEcosystemIsTheFullPrompt(t *testing.T) {
	out, ok := prompts.IntegrationFor("http://example.local", "")
	if !ok || out != prompts.Integration("http://example.local") {
		t.Fatalf("empty ecosystem should render the unscoped prompt")
	}
}

func TestIntegrationFor_ScopesToOneEcosystem(t *testing.T) {
	out, ok := prompts.IntegrationFor("http://example.local/", "python")
	if !ok {
		t.Fatal("python should be a known ecosystem")
	}
	scope := strings.Index(out, "## Scope — Python only")
	first := strings.Index(out, "## Identification")
	if scope == -1 || first == -1 || scope > first {
		t.Fatalf("scope section must come before the first instruction section; scope=%d identification=%d", scope, first)
	}
	for _, s := range []string{
		"`http://example.local/pypi/simple/` and `http://example.local/conda/`",
		"pip, uv, Poetry, Pipenv, PDM, Conda",
		"Leave every other ecosystem's configuration",
		// The safety sections stay: a scoped run follows the same rules.
		"Public-registry fallback",
		"Hard constraints",
	} {
		if !strings.Contains(out, s) {
			t.Errorf("scoped prompt missing %q", s)
		}
	}
}

func TestIntegrationFor_RejectsUnknownEcosystem(t *testing.T) {
	if _, ok := prompts.IntegrationFor("http://example.local", "cobol"); ok {
		t.Fatal("unknown ecosystem should be rejected")
	}
}

func TestIntegrationFor_EveryEcosystemRenders(t *testing.T) {
	for _, id := range prompts.Ecosystems() {
		out, ok := prompts.IntegrationFor("http://example.local", id)
		if !ok || !strings.Contains(out, "## Scope — ") {
			t.Errorf("%s: scoped prompt not rendered", id)
		}
	}
}
