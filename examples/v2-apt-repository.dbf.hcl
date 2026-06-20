# DebianForm v2 APT repository 示例。
#
# 该示例覆盖已实现的 host/profile 级 apt.repository、显式 package repository
# 依赖和 host-scoped APT cache refresh。
#
# - APT repository 是 host/profile/component 内的 apt 领域对象。
# - package 只依赖自己显式引用的 repository。
# - 同一 host 多个 repository 变化时，只生成一次 APT cache refresh operation。

host "apt1" {
  ssh {
    host = "apt1"
  }

  system {
    hostname     = "apt1"
    architecture = "amd64"
    codename     = "trixie"
  }

  apt {
    repository "example_tools" {
      uris       = ["https://example.com/debian"]
      suites     = ["trixie"]
      components = ["main"]

      signing_key {
        url    = "https://example.com/debian/repository.asc"
        sha256 = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
        path   = "/etc/apt/keyrings/example-tools.asc"
      }
    }
  }

  packages {
    install = [
      "curl",
    ]

    package "example-tool" {
      repositories = ["example_tools"]
    }
  }
}

# 预期资源图：
#
# host.apt1.apt.signing_key["example_tools"]
#   -> host.apt1.apt.repository["example_tools"]
#   -> host.apt1.apt.cache_refresh
#   -> host.apt1.packages.install["example-tool"]
#
# host.apt1.packages.install["curl"] 不依赖 example_tools。
