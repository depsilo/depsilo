package ecosystem

// DefaultUpstream is a built-in Upstream for an ecosystem the first-run
// wizard offers. The wizard proposes these, and an instance that has no
// config file yet serves from them until setup writes the operator's choice.
type DefaultUpstream struct {
	Name     string `json:"name"`
	URL      string `json:"url"`
	Priority int    `json:"priority"`
}

// Regional mirrors come first for the networks Depsilo is mostly deployed in;
// the official registry follows as a fallback where it is reachable.
var defaultUpstreams = map[string][]DefaultUpstream{
	"pypi": {
		{Name: "tuna", URL: "https://pypi.tuna.tsinghua.edu.cn", Priority: 1},
		{Name: "official", URL: "https://pypi.org", Priority: 2},
	},
	"apt": {
		{Name: "tuna", URL: "https://mirrors.tuna.tsinghua.edu.cn", Priority: 1},
		{Name: "aliyun", URL: "https://mirrors.aliyun.com", Priority: 2},
	},
	"npm": {
		{Name: "npmmirror", URL: "https://registry.npmmirror.com", Priority: 1},
		{Name: "official", URL: "https://registry.npmjs.org", Priority: 2},
	},
	"go": {
		{Name: "goproxy-cn", URL: "https://goproxy.cn", Priority: 1},
		{Name: "official", URL: "https://proxy.golang.org", Priority: 2},
	},
	"cargo": {
		{Name: "rsproxy", URL: "https://rsproxy.cn/index/", Priority: 1},
		{Name: "crates-io", URL: "https://index.crates.io", Priority: 2},
	},
	"maven": {
		{Name: "aliyun", URL: "https://maven.aliyun.com/repository/public", Priority: 1},
		{Name: "central", URL: "https://repo.maven.apache.org/maven2/", Priority: 2},
	},
	"rubygems": {
		{Name: "tuna", URL: "https://mirrors.tuna.tsinghua.edu.cn/rubygems/", Priority: 1},
		{Name: "rubygems", URL: "https://rubygems.org", Priority: 2},
	},
	"composer": {
		{Name: "aliyun", URL: "https://mirrors.aliyun.com/composer/", Priority: 1},
		{Name: "packagist", URL: "https://repo.packagist.org", Priority: 2},
	},
	"nuget": {
		{Name: "nuget", URL: "https://api.nuget.org", Priority: 1},
	},
	"conda": {
		{Name: "tuna", URL: "https://mirrors.tuna.tsinghua.edu.cn/anaconda", Priority: 1},
		{Name: "official", URL: "https://repo.anaconda.com", Priority: 2},
	},
	"cran": {
		{Name: "tuna", URL: "https://mirrors.tuna.tsinghua.edu.cn/CRAN", Priority: 1},
		{Name: "cloud-r", URL: "https://cloud.r-project.org", Priority: 2},
	},
	"alpine": {
		{Name: "tuna", URL: "https://mirrors.tuna.tsinghua.edu.cn/alpine", Priority: 1},
		{Name: "official", URL: "https://dl-cdn.alpinelinux.org/alpine", Priority: 2},
	},
	"helm": {
		{Name: "azure-cn", URL: "https://mirror.azure.cn/kubernetes/charts", Priority: 1},
		{Name: "bitnami", URL: "https://charts.bitnami.com/bitnami", Priority: 2},
	},
}

// DefaultUpstreams returns a copy of the built-in Upstreams for name, in
// priority order, or nil when the wizard does not offer that ecosystem.
func DefaultUpstreams(name string) []DefaultUpstream {
	upstreams := defaultUpstreams[name]
	if len(upstreams) == 0 {
		return nil
	}
	return append([]DefaultUpstream(nil), upstreams...)
}
