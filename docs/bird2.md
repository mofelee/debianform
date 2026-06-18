# 示例:用 DebianForm 安装 BIRD2

本文展示按最新模块设计标准安装 BIRD2 的目标写法。完整配置见
[examples/bird2.dbf.hcl](../examples/bird2.dbf.hcl)。

当前实现已支持这个示例所需的高层 APT repository、远端 key 获取和服务包依赖字段。

## 目标状态

用户只声明系统应该是什么样：

- 主机有 CZ.NIC 的 BIRD2 APT 仓库。
- 仓库 signing key 来自 `https://pkg.labs.nic.cz/gpg`。
- 主机安装 `bird2` 包。
- `bird` 服务启用并运行。

```hcl
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
```

## 自动推导

用户不需要声明目录、key 文件、`apt-get update` 或资源顺序。provider 和引擎内部
应推导出类似下面的图：

```text
package:bird_host:ca-certificates
  -> tls-ca:bird_host
  -> fetch_apt_key[bird_host, https://pkg.labs.nic.cz/gpg]
  -> path:bird_host:/etc/apt/keyrings/cznic.asc
  -> apt-repository:bird_host:cznic_bird2
  -> apt_update[bird_host]
  -> package:bird_host:bird2
  -> service:bird_host:bird
```

关键规则：

- `key.url` 使用 HTTPS，所以自动依赖 `ca-certificates`。
- `/etc/apt/keyrings` 由 repository provider 自动创建。
- signing key 和 deb822 source 文件由 `debian_apt_repository` 管理。
- APT repository 变化后自动触发 host 级 `apt_update[host]`，同一主机去重一次。
- `debian_package "bird2"` 不需要 `update_cache = true`。
- `debian_service "bird"` 通过 `package = "bird2"` 表达语义依赖，不需要 `depends_on`。

## 调整系统版本

仓库的 `suites` 用 `local.suite` 控制。示例默认为 Debian 13 `trixie`，换成目标系统的
codename 即可：

```hcl
locals {
  suite = "bookworm"
}
```

## 使用

使用方式保持普通 `dbf` 工作流：

```bash
dbf plan  -f examples/bird2.dbf.hcl
dbf apply -f examples/bird2.dbf.hcl
dbf check -f examples/bird2.dbf.hcl
```
