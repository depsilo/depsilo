package prompts

import (
	"net/url"
	"strings"
)

// bootstrapSetting is the bootstrap prompt's entry for one ecosystem. Every
// setting it names is one this repository already maintains: the Portal's
// catalog in web/src/lib/ecosystemData.ts, the depsilo_configure MCP tool, or
// the real-client images under testground/.
type bootstrapSetting struct {
	// ecosystem is the adapter name, as /api/v1/discover reports it.
	ecosystem string
	// text is the entry; {{MIRROR_URL}} stands for the service origin.
	text string
}

// bootstrapSettings is in the order the prompt lists them. Each line says
// where the setting lives, because the prompt's first rule depends on it.
var bootstrapSettings = []bootstrapSetting{
	{"pypi", `   pip      command:      PIP_INDEX_URL={{MIRROR_URL}}/pypi/simple/ pip install <pkg>
            user-level:   pip config set global.index-url {{MIRROR_URL}}/pypi/simple/`},
	{"npm", `   npm      project:      .npmrc: registry={{MIRROR_URL}}/npm/ (see the lockfile note below)
            command:      npm install --registry={{MIRROR_URL}}/npm/
            user-level:   npm config set registry {{MIRROR_URL}}/npm/`},
	{"go", `   go       command:      GOPROXY={{MIRROR_URL}}/go,direct go build ./...
            user-level:   go env -w GOPROXY={{MIRROR_URL}}/go,direct`},
	{"cargo", `   cargo    project:      .cargo/config.toml (user-level: ~/.cargo/config.toml)
              [source.crates-io]
              replace-with = "depsilo"
              [source.depsilo]
              registry = "sparse+{{MIRROR_URL}}/crates/"`},
	{"maven", `   maven    user-level:   ~/.m2/settings.xml, inside <mirrors>:
              <mirror><id>depsilo</id><mirrorOf>*</mirrorOf><url>{{MIRROR_URL}}/maven/</url></mirror>`},
	{"rubygems", `   gem      command:      gem install --source {{MIRROR_URL}}/rubygems/ <gem>
   bundler  project:      bundle config set --local mirror.https://rubygems.org {{MIRROR_URL}}/rubygems/
            user-level:   bundle config set --global mirror.https://rubygems.org {{MIRROR_URL}}/rubygems/
            (check "bundle --version" first: before 2.1, leave out "set")`},
	{"composer", `   composer user-level:   composer config -g repo.packagist composer {{MIRROR_URL}}/composer/`},
	{"nuget", `   nuget    command:      dotnet restore --source {{MIRROR_URL}}/nuget/v3/index.json
            user-level:   dotnet nuget add source {{MIRROR_URL}}/nuget/v3/index.json -n depsilo`},
	{"conda", `   conda    command:      conda install --override-channels -c {{MIRROR_URL}}/conda/pkgs/main <pkg>
            user-level:   conda config --set repodata_use_zst false (if repodata.json.zst is 404)`},
	{"helm", `   helm     user-level:   helm repo add depsilo {{MIRROR_URL}}/helm/`},
	{"huggingface", `   hf       command:      HF_ENDPOINT={{MIRROR_URL}}/huggingface hf download <model>`},
	{"cran", `   R/CRAN   project:      .Rprofile: options(repos = c(CRAN = "{{MIRROR_URL}}/cran/"))`},
	{"apt", `   apt      system-level: /etc/apt/sources.list: replace the host with {{MIRROR_URL}}/apt
            (for example {{MIRROR_URL}}/apt/debian); keep signature checking on`},
	{"alpine", `   alpine   system-level: /etc/apk/repositories: {{MIRROR_URL}}/alpine/v<major.minor>/main
            and {{MIRROR_URL}}/alpine/v<major.minor>/community`},
	{"docker", `   docker   system-level: /etc/docker/daemon.json {"registry-mirrors": ["{{MIRROR_URL}}"]}
            (the service root, no path); show it to the operator, do not edit it`},
}

// bootstrapPlainHTTPNotes extend an entry when the service address is plain
// HTTP; HTTPS needs none of them. The opt-ins are the Portal's
// (web/src/lib/ecosystemData.ts).
var bootstrapPlainHTTPNotes = map[string]string{
	// pip ignores an http:// index on a non-loopback host unless the host is
	// trusted, so without it the two lines above fail on a LAN instance.
	// {{MIRROR_HOST}} is the host[:port] of the address.
	"pypi": `
            plain HTTP:   pip skips this index unless the host is trusted; add
                          PIP_TRUSTED_HOST={{MIRROR_HOST}} to the command, or
            user-level:   pip config set global.trusted-host {{MIRROR_HOST}}`,
	// Composer refuses http:// repositories while secure-http is on.
	"composer": `
            plain HTTP:   Composer refuses this URL unless you also run
            user-level:   composer config -g secure-http false`,
	// .NET SDK 9 fails an http source with NU1302 unless it opts in; the
	// flag arrived in SDK 9, and earlier SDKs reject it. "dotnet restore" has
	// no such flag, so the command line above cannot opt in.
	"nuget": `
            plain HTTP:   .NET SDK 9 refuses this source (NU1302), and the
                          command line cannot opt in: append
                          --allow-insecure-connections to the user-level line
                          (SDK 8 and earlier reject that flag)`,
}

// bootstrapOtherClients end the npm and pip entries: the other clients of the
// ecosystem read settings of their own, which the integration guide's table
// gives. The guide's scope ids are the Portal's, not the adapter names.
var bootstrapOtherClients = map[string]string{
	"pypi": `
            uv, Poetry, PDM, Pipenv: see {{MIRROR_URL}}/api/v1/integration-prompt?ecosystem=python`,
	"npm": `
            pnpm, Yarn, Bun: see {{MIRROR_URL}}/api/v1/integration-prompt?ecosystem=node`,
}

// bootstrapNpmLockfileNote follows the list when npm is served. Depsilo's npm
// route serves only the signed tarball URLs of its rewritten metadata. npm
// moves registry.npmjs.org URLs of a lockfile to the configured registry, so
// those fail through Depsilo; URLs of other registries are followed as
// recorded and bypass it.
const bootstrapNpmLockfileNote = `   npm lockfiles: a project that commits package-lock.json should also set
   omit-lockfile-registry-resolved=true in its .npmrc. An existing lockfile
   whose "resolved" URLs point at registry.npmjs.org fails through Depsilo
   with 404; one whose URLs point at another registry installs from there and
   bypasses Depsilo. Rewrite either with
   "npm install --package-lock-only --omit-lockfile-registry-resolved" (adding
   the flag to "npm ci" alone does not help); ask before rewriting it.
   pnpm, Yarn 2+ and Bun lockfiles can fail the same way and have no such
   flag: report them; do not regenerate one without approval.

`

// Bootstrap renders the bootstrap prompt: what a coding agent should do on a
// developer's machine so this workspace's installs go through the Depsilo
// instance at baseURL. GET /api/v1/agent-prompt serves it and "depsilo
// init-agent" writes it into a project.
//
// ecosystems are the adapter names the instance serves, as /api/v1/discover
// reports them; only those are listed. mcpTools names the instance's MCP
// tools and may be empty.
//
// Distinct from Integration, which rewrites a project's build and CI files.
func Bootstrap(baseURL string, ecosystems, mcpTools []string) string {
	return renderBootstrap(baseURL, ecosystems, mcpTools, false)
}

// BootstrapOffline renders the bootstrap prompt without knowing the instance:
// it lists every ecosystem and says so. "depsilo init-agent" uses it when the
// instance cannot be reached.
func BootstrapOffline(baseURL string) string {
	all := make([]string, 0, len(bootstrapSettings))
	for _, setting := range bootstrapSettings {
		all = append(all, setting.ecosystem)
	}
	return renderBootstrap(baseURL, all, nil, true)
}

func renderBootstrap(baseURL string, ecosystems, mcpTools []string, offline bool) string {
	served := make(map[string]bool, len(ecosystems))
	for _, name := range ecosystems {
		served[name] = true
	}
	base := strings.TrimRight(baseURL, "/")
	plainHTTPHost := ""
	if parsed, err := url.Parse(base); err == nil && parsed.Scheme == "http" {
		plainHTTPHost = parsed.Host
	}
	var entries []string
	for _, setting := range bootstrapSettings {
		if !served[setting.ecosystem] {
			continue
		}
		text := setting.text
		if plainHTTPHost != "" {
			text += bootstrapPlainHTTPNotes[setting.ecosystem]
		}
		text += bootstrapOtherClients[setting.ecosystem]
		entries = append(entries, text)
	}

	var out strings.Builder
	out.WriteString(`This workspace has a dependency cache called Depsilo at {{MIRROR_URL}}.

Rule for everything below: prefer a setting that lives in this project or
applies to a single command. Before writing user-level or system-level
configuration (anything under the home directory or /etc, including what
"config set", "env -w" and "-g" commands write), show the user the exact
changes and wait for approval.

1. Check that it is reachable: curl -sf {{MIRROR_URL}}/health

2. Configure only the package managers this project uses. `)
	switch {
	case offline:
		out.WriteString(`This text was written
   without reaching the instance, so it lists every ecosystem Depsilo can
   serve; {{MIRROR_URL}}/api/v1/discover names the ones this instance serves.`)
	case len(entries) == 0:
		out.WriteString(`This instance
   reports no package ecosystems: configure nothing and tell the user.`)
	default:
		out.WriteString(`This instance serves:`)
	}
	out.WriteString("\n\n")
	if len(entries) > 0 {
		out.WriteString(strings.Join(entries, "\n"))
		out.WriteString("\n\n")
	}
	if served["npm"] {
		out.WriteString(bootstrapNpmLockfileNote)
	}
	out.WriteString(`   Dockerfiles and CI files have their own guide:
   {{MIRROR_URL}}/api/v1/integration-prompt`)
	// The guide's scope ids are the Portal's, not the adapter names.
	var scopes []string
	for _, scope := range [][2]string{{"apt", "debian (apt)"}, {"alpine", "alpine (apk)"}, {"docker", "container (Docker)"}} {
		if served[scope[0]] {
			scopes = append(scopes, scope[1])
		}
	}
	if len(scopes) > 0 {
		out.WriteString("\n   Add ?ecosystem=<id> for one of: " + strings.Join(scopes, ", ") + ".")
	}
	out.WriteString(`

3. Run install commands as usual.

These settings do not provide reliable outage failover. Keep the original
registry settings as documented rollback instructions.`)
	if served["go"] {
		out.WriteString(` Even Go's ",direct"
suffix advances only after a 404/410 response, not when Depsilo is
unreachable; do not use GOPROXY "|direct", which would also bypass Depsilo's
451 enforcement.`)
	}
	out.WriteString(`

Native MCP integration (preferred for MCP-aware agents like Claude Code,
Cursor): create a read-only API token in Admin > Users, point your client at
{{MIRROR_URL}}/mcp, and send it as an Authorization: Bearer header. POST
initialize to begin, then tools/list to enumerate`)
	if len(mcpTools) > 0 {
		out.WriteString(". Available tools:\n" + wrapCommaList(mcpTools, 78))
	}
	out.WriteString(".\n")

	return strings.NewReplacer("{{MIRROR_URL}}", base, "{{MIRROR_HOST}}", plainHTTPHost).Replace(out.String())
}

// wrapCommaList joins items with ", " and breaks the result into lines of at
// most width columns, matching the hard-wrapped prompt text around it.
func wrapCommaList(items []string, width int) string {
	var out strings.Builder
	column := 0
	for index, item := range items {
		if index < len(items)-1 {
			item += ","
		}
		switch {
		case column == 0:
		case column+1+len(item) > width:
			out.WriteByte('\n')
			column = 0
		default:
			out.WriteByte(' ')
			column++
		}
		out.WriteString(item)
		column += len(item)
	}
	return out.String()
}
