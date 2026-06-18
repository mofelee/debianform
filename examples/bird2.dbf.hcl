# 用 DebianForm 的目标高层模型从 CZ.NIC 官方仓库安装 BIRD2。
#
# 这个示例遵循 docs/module-design.md 的最新模块设计标准：
#   - 用户声明系统事实，不声明执行步骤。
#   - APT signing key 从 HTTPS 远端获取时，provider 自动依赖 ca-certificates。
#   - APT source 变化后，provider 自动触发 host 级 apt_update[host]。
#   - 服务通过 package 字段表达语义依赖，不手写 depends_on。
#
# 注意：debian_apt_repository、key.url、service.package 和内部图调度仍是目标设计，
# 当前实现完成前，现有 dbf 可能会拒绝这个示例。

state "ssh" {
  host      = "bird_host"
  path      = "/var/lib/debianform/bird2-state.json"
  lock_path = "/var/lock/debianform/bird2-state.lock"
}

locals {
  # Debian 13 = trixie。换成目标系统的 codename 即可，例如 bookworm。
  suite = "trixie"
}

debian_apt_repository "cznic_bird2" {
  host       = "bird_host"
  uris       = "https://pkg.labs.nic.cz/bird2"
  suites     = local.suite
  components = "main"

  key = {
    url  = "https://pkg.labs.nic.cz/gpg"
    path = "/etc/apt/keyrings/cznic.asc"
  }
}

debian_package "bird2" {
  host = "bird_host"
  name = "bird2"
}

debian_service "bird" {
  host    = "bird_host"
  name    = "bird"
  package = "bird2"
  enabled = true
  state   = "running"
}
