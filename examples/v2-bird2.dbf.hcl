# DebianForm v2 BIRD2 component 示例。
#
# 该示例覆盖已实现的领域型 component 展开、target 上下文、APT repository
# 和 package/service 依赖。
#
# - APT repository 是 apt 领域对象，不是顶层 component。
# - bird2 component 封装 repository、package 和 service。
# - package 只依赖自己显式引用的 repository。
# - repository 变化后，编译器为目标 host 生成一次 APT cache refresh。

component "bird2" {
  apt {
    repository "cznic_bird2" {
      uris       = ["https://pkg.labs.nic.cz/bird2"]
      suites     = [target.system.codename]
      components = ["main"]

      signing_key {
        url    = "https://pkg.labs.nic.cz/gpg"
        sha256 = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
        path   = "/etc/apt/keyrings/cznic-bird2.asc"
      }
    }
  }

  packages {
    package "bird2" {
      repositories = ["cznic_bird2"]
    }
  }

  services {
    service "bird" {
      package = "bird2"
      enabled = true
      state   = "running"
    }
  }
}

host "router1" {
  components = [
    component.bird2,
  ]

  ssh {
    host = "router1"
  }

  system {
    hostname     = "router1"
    architecture = "amd64"
    codename     = "trixie"
  }
}

# 预期资源图：
#
# host.router1.components.bird2.apt.signing_key["cznic_bird2"]
#   -> host.router1.components.bird2.apt.repository["cznic_bird2"]
#   -> host.router1.apt.cache_refresh
#   -> host.router1.components.bird2.packages.install["bird2"]
#   -> host.router1.components.bird2.services.service["bird"]
