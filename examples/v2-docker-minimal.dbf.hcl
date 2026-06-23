# DebianForm v2 Docker minimal 示例。
#
# 最小语法会展开为 Docker 官方 APT 源、默认 packages 和 docker.service。
# 离线 plan 需要显式声明目标 Debian runtime facts。

host "docker1" {
  system {
    architecture = "amd64"
    codename     = "trixie"
  }

  docker {
    enable = true
  }
}
