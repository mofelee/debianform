# DebianForm v2 APT repository 设计示例。
#
# 设计夹具：apt.repository 会在后续 loop 接入 CLI。
#
# 设计边界：
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
        sha256 = "REPLACE_WITH_EXAMPLE_TOOLS_KEY_SHA256"
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
