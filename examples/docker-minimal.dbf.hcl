# DebianForm Docker minimal 示例。
#
# 最小语法会展开为 Docker 官方 APT 源、默认 packages 和 docker.service。
# 在线 plan 会自动发现目标 Debian platform facts。若要离线 plan，
# 请在 host 中显式声明 platform.architecture 和 platform.codename。

host "docker1" {
  docker {
    enable = true
  }
}
