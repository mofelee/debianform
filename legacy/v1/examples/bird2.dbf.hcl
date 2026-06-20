# 用 DebianForm 的高层 APT repository 模型从 CZ.NIC 官方仓库安装 BIRD2。
#
# 这个示例遵循 README.md 中的模块设计原则：
#   - 用户声明系统事实，不声明执行步骤。
#   - APT signing key 从 HTTPS 远端获取时，provider 自动依赖 ca-certificates。
#   - APT source 变化后，provider 自动触发 host 级 apt_update[host]。
#   - 服务通过 package 字段表达语义依赖，不手写 depends_on。
#
# 当前实现已支持这个示例所需的 debian_apt_repository、key.url 和 service.package。

locals {
  # Debian 13 = trixie。换成目标系统的 codename 即可，例如 bookworm。
  host = "ksvm202"
  suite = "trixie"
}

state "ssh" {
  host      = local.host
  path      = "/var/lib/debianform/bird2-state.json"
  lock_path = "/var/lock/debianform/bird2-state.lock"
}

debian_apt_repository "cznic_bird2" {
  host       = local.host
  uris       = "https://pkg.labs.nic.cz/bird2"
  suites     = local.suite
  components = "main"

  key = {
    url  = "https://pkg.labs.nic.cz/gpg"
    path = "/etc/apt/keyrings/cznic.asc"
  }
}

debian_package "bird2" {
  host = local.host
  name = "bird2"
}

debian_service "bird" {
  host    = local.host
  name    = "bird"
  package = "bird2"
  enabled = true
  state   = "running"
}
