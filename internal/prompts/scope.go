package prompts

import (
	"fmt"
	"sort"
	"strings"
)

// EcosystemScope narrows the integration prompt to one ecosystem. The keys
// are the Portal's ecosystem ids, so the Portal can ask for the prompt of the
// ecosystem a user is looking at.
type EcosystemScope struct {
	// Name is the ecosystem as users say it, e.g. "Python".
	Name string
	// Clients are the install tools this scope covers.
	Clients []string
	// Paths are this service's routes for the ecosystem, appended to the
	// mirror URL; an empty path means the service root.
	Paths []string
}

var ecosystemScopes = map[string]EcosystemScope{
	"python":      {Name: "Python", Clients: []string{"pip", "uv", "Poetry", "Pipenv", "PDM", "Conda"}, Paths: []string{"/pypi/simple/", "/conda/"}},
	"node":        {Name: "Node.js", Clients: []string{"npm", "pnpm", "Yarn", "Bun"}, Paths: []string{"/npm/"}},
	"java":        {Name: "Java", Clients: []string{"Maven", "Gradle", "sbt"}, Paths: []string{"/maven/"}},
	"rust":        {Name: "Rust", Clients: []string{"Cargo"}, Paths: []string{"/crates/"}},
	"go":          {Name: "Go", Clients: []string{"go"}, Paths: []string{"/go"}},
	"container":   {Name: "Container", Clients: []string{"Docker", "containerd", "Podman"}, Paths: []string{""}},
	"kubernetes":  {Name: "Kubernetes", Clients: []string{"Helm"}, Paths: []string{"/helm/"}},
	"ruby":        {Name: "Ruby", Clients: []string{"gem", "Bundler"}, Paths: []string{"/rubygems/"}},
	"dotnet":      {Name: ".NET", Clients: []string{"dotnet CLI", "NuGet"}, Paths: []string{"/nuget/v3/index.json"}},
	"debian":      {Name: "Debian / Ubuntu", Clients: []string{"apt"}, Paths: []string{"/apt/<distro>"}},
	"alpine":      {Name: "Alpine", Clients: []string{"apk"}, Paths: []string{"/alpine/<version>/<repo>"}},
	"php":         {Name: "PHP", Clients: []string{"Composer"}, Paths: []string{"/composer/"}},
	"r":           {Name: "R", Clients: []string{"R"}, Paths: []string{"/cran/"}},
	"huggingface": {Name: "Hugging Face", Clients: []string{"hf", "transformers"}, Paths: []string{"/huggingface/"}},
}

// Ecosystems lists the ids IntegrationFor accepts, sorted.
func Ecosystems() []string {
	ids := make([]string, 0, len(ecosystemScopes))
	for id := range ecosystemScopes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// IntegrationFor renders the integration prompt for one ecosystem: the full
// prompt with a scope section after its opening paragraph that limits the
// agent to that ecosystem. An empty ecosystem returns the unscoped prompt;
// an unknown one reports ok=false.
func IntegrationFor(baseURL, ecosystem string) (string, bool) {
	if ecosystem == "" {
		return Integration(baseURL), true
	}
	scope, ok := ecosystemScopes[ecosystem]
	if !ok {
		return "", false
	}
	baseURL = strings.TrimRight(baseURL, "/")
	body := Integration(baseURL)

	addresses := make([]string, len(scope.Paths))
	for i, path := range scope.Paths {
		addresses[i] = "`" + baseURL + path + "`"
	}
	section := fmt.Sprintf(`## Scope — %[1]s only

This run covers **%[1]s** dependency installation only (%[2]s). This service's %[1]s address is %[3]s.

Leave every other ecosystem's configuration exactly as it is, even where the sections below describe it. If you notice other ecosystems, list them in your report instead of wiring them.

`, scope.Name, strings.Join(scope.Clients, ", "), strings.Join(addresses, " and "))

	// The scope goes before the first section so the agent reads it before
	// any per-ecosystem instruction.
	if i := strings.Index(body, "\n## "); i != -1 {
		return body[:i+1] + section + body[i+1:], true
	}
	return section + body, true
}
