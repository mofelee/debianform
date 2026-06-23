# DebianForm v2 Docker minimal 示例。
#
# Loop 1 只完成 docker DSL 的 validate / HostSpec 编译。
# 后续 loop 会把该高阶块展开为 Docker 官方 APT 源、packages 和 docker.service。

host "docker1" {
  docker {
    enable = true
  }
}
