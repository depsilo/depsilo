export interface UpstreamDefault {
  name: string
  url: string
  priority: number
  proxy?: string
}

export interface EcosystemDefault {
  key: string
  label: string
  upstreams: UpstreamDefault[]
}

export const ecosystemDefaults: EcosystemDefault[] = [
  {
    key: 'pypi',
    label: 'PyPI (pip)',
    upstreams: [
      { name: 'tuna', url: 'https://pypi.tuna.tsinghua.edu.cn', priority: 1 },
      { name: 'official', url: 'https://pypi.org', priority: 2 },
    ],
  },
  {
    key: 'apt',
    label: 'APT (Debian/Ubuntu)',
    upstreams: [
      { name: 'tuna', url: 'https://mirrors.tuna.tsinghua.edu.cn', priority: 1 },
      { name: 'aliyun', url: 'https://mirrors.aliyun.com', priority: 2 },
    ],
  },
  {
    key: 'npm',
    label: 'npm (Node.js)',
    upstreams: [
      { name: 'npmmirror', url: 'https://registry.npmmirror.com', priority: 1 },
      { name: 'official', url: 'https://registry.npmjs.org', priority: 2 },
    ],
  },
  {
    key: 'go',
    label: 'Go Modules',
    upstreams: [
      { name: 'goproxy-cn', url: 'https://goproxy.cn', priority: 1 },
      { name: 'official', url: 'https://proxy.golang.org', priority: 2 },
    ],
  },
  {
    key: 'cargo',
    label: 'Cargo (Rust)',
    upstreams: [
      { name: 'rsproxy', url: 'https://rsproxy.cn/index', priority: 1 },
      { name: 'crates-io', url: 'https://index.crates.io', priority: 2 },
    ],
  },
  {
    key: 'maven',
    label: 'Maven (Java)',
    upstreams: [
      { name: 'aliyun', url: 'https://maven.aliyun.com/repository/public', priority: 1 },
      { name: 'central', url: 'https://repo1.maven.org/maven2', priority: 2 },
    ],
  },
  {
    key: 'rubygems',
    label: 'RubyGems',
    upstreams: [
      { name: 'ruby-china', url: 'https://gems.ruby-china.com', priority: 1 },
      { name: 'rubygems', url: 'https://rubygems.org', priority: 2 },
    ],
  },
  {
    key: 'composer',
    label: 'Composer (PHP)',
    upstreams: [
      { name: 'aliyun', url: 'https://mirrors.aliyun.com/composer', priority: 1 },
      { name: 'packagist', url: 'https://repo.packagist.org', priority: 2 },
    ],
  },
  {
    key: 'nuget',
    label: 'NuGet (.NET)',
    upstreams: [
      { name: 'nuget', url: 'https://api.nuget.org', priority: 1 },
    ],
  },
  {
    key: 'conda',
    label: 'Conda',
    upstreams: [
      { name: 'tuna', url: 'https://mirrors.tuna.tsinghua.edu.cn/anaconda', priority: 1 },
      { name: 'official', url: 'https://repo.anaconda.com', priority: 2 },
    ],
  },
  {
    key: 'cran',
    label: 'CRAN (R)',
    upstreams: [
      { name: 'tuna', url: 'https://mirrors.tuna.tsinghua.edu.cn/CRAN', priority: 1 },
      { name: 'cloud-r', url: 'https://cloud.r-project.org', priority: 2 },
    ],
  },
  {
    key: 'helm',
    label: 'Helm (Kubernetes)',
    upstreams: [
      { name: 'azure-cn', url: 'https://mirror.azure.cn/kubernetes/charts', priority: 1 },
      { name: 'bitnami', url: 'https://charts.bitnami.com/bitnami', priority: 2 },
    ],
  },
  {
    key: 'docker',
    label: 'Docker',
    upstreams: [],
  },
]
