// Code generated from internal/ecosystem/defaults.go by make generate. DO NOT EDIT.

export const setupUpstreamDefaults = [
  {
    "key": "pypi",
    "upstreams": [
      {
        "name": "tuna",
        "url": "https://pypi.tuna.tsinghua.edu.cn",
        "priority": 1
      },
      {
        "name": "official",
        "url": "https://pypi.org",
        "priority": 2
      }
    ]
  },
  {
    "key": "apt",
    "upstreams": [
      {
        "name": "tuna",
        "url": "https://mirrors.tuna.tsinghua.edu.cn",
        "priority": 1
      },
      {
        "name": "aliyun",
        "url": "https://mirrors.aliyun.com",
        "priority": 2
      }
    ]
  },
  {
    "key": "npm",
    "upstreams": [
      {
        "name": "npmmirror",
        "url": "https://registry.npmmirror.com",
        "priority": 1
      },
      {
        "name": "official",
        "url": "https://registry.npmjs.org",
        "priority": 2
      }
    ]
  },
  {
    "key": "go",
    "upstreams": [
      {
        "name": "goproxy-cn",
        "url": "https://goproxy.cn",
        "priority": 1
      },
      {
        "name": "official",
        "url": "https://proxy.golang.org",
        "priority": 2
      }
    ]
  },
  {
    "key": "cargo",
    "upstreams": [
      {
        "name": "rsproxy",
        "url": "https://rsproxy.cn/index/",
        "priority": 1
      },
      {
        "name": "crates-io",
        "url": "https://index.crates.io",
        "priority": 2
      }
    ]
  },
  {
    "key": "maven",
    "upstreams": [
      {
        "name": "aliyun",
        "url": "https://maven.aliyun.com/repository/public",
        "priority": 1
      },
      {
        "name": "central",
        "url": "https://repo.maven.apache.org/maven2/",
        "priority": 2
      }
    ]
  },
  {
    "key": "rubygems",
    "upstreams": [
      {
        "name": "tuna",
        "url": "https://mirrors.tuna.tsinghua.edu.cn/rubygems/",
        "priority": 1
      },
      {
        "name": "rubygems",
        "url": "https://rubygems.org",
        "priority": 2
      }
    ]
  },
  {
    "key": "composer",
    "upstreams": [
      {
        "name": "aliyun",
        "url": "https://mirrors.aliyun.com/composer/",
        "priority": 1
      },
      {
        "name": "packagist",
        "url": "https://repo.packagist.org",
        "priority": 2
      }
    ]
  },
  {
    "key": "nuget",
    "upstreams": [
      {
        "name": "nuget",
        "url": "https://api.nuget.org",
        "priority": 1
      }
    ]
  },
  {
    "key": "conda",
    "upstreams": [
      {
        "name": "tuna",
        "url": "https://mirrors.tuna.tsinghua.edu.cn/anaconda",
        "priority": 1
      },
      {
        "name": "official",
        "url": "https://repo.anaconda.com",
        "priority": 2
      }
    ]
  },
  {
    "key": "cran",
    "upstreams": [
      {
        "name": "tuna",
        "url": "https://mirrors.tuna.tsinghua.edu.cn/CRAN",
        "priority": 1
      },
      {
        "name": "cloud-r",
        "url": "https://cloud.r-project.org",
        "priority": 2
      }
    ]
  },
  {
    "key": "alpine",
    "upstreams": [
      {
        "name": "tuna",
        "url": "https://mirrors.tuna.tsinghua.edu.cn/alpine",
        "priority": 1
      },
      {
        "name": "official",
        "url": "https://dl-cdn.alpinelinux.org/alpine",
        "priority": 2
      }
    ]
  },
  {
    "key": "helm",
    "upstreams": [
      {
        "name": "azure-cn",
        "url": "https://mirror.azure.cn/kubernetes/charts",
        "priority": 1
      },
      {
        "name": "bitnami",
        "url": "https://charts.bitnami.com/bitnami",
        "priority": 2
      }
    ]
  }
] as const
