# 示例:用 DebianForm 安装 BIRD2

本文演示如何把一段常见的命令式安装脚本翻译成 DebianForm 的声明式配置。
完整配置见 [examples/bird2.dbf.hcl](../examples/bird2.dbf.hcl)。

注意：本文反映当前已实现的低层资源能力，用于解释脚本如何迁移到 DebianForm。
新增模块和后续高层资源设计不应以本文的顺序链为标准；模块设计准则见
[module-design.md](module-design.md)。

## 要实现的功能

从 CZ.NIC 官方仓库安装 BIRD2,等价于下面这段脚本做的事:

1. 安装前置依赖(根证书等)。
2. 下载 CZ.NIC 的 GPG 公钥,`gpg --dearmor` 成二进制 keyring。
3. 写入 APT 仓库定义,`signed-by` 指向该 keyring。
4. `apt update && apt install -y bird2`。
5. 启用并启动 `bird` 服务。

## 命令式 vs. 声明式

命令式脚本是「一步步怎么做」;DebianForm 描述的是「最终应该是什么样」,
然后由 `dbf` 计算差异并收敛。每次 `apply` 都是幂等的,`dbf check` 还能检测漂移。

脚本与资源的对应关系:

| 脚本步骤 | DebianForm 资源 |
| --- | --- |
| `apt install ca-certificates` | `debian_package.ca_certificates` |
| `mkdir -p /etc/apt/keyrings` | `debian_directory.keyrings` |
| 下载并 `gpg --dearmor` 公钥 | `debian_file.cznic_key` |
| 写 `/etc/apt/sources.list.d/*.list` | `debian_apt_source.cznic_bird2` |
| `apt update && apt install bird2` | `debian_package.bird2`(`update_cache = true`) |
| `systemctl enable --now bird` | `debian_service.bird` |

### 关于 GPG 公钥:不需要 dearmor

脚本里用 `gpg --dearmor` 把 ASCII-armored 公钥转成二进制 keyring,这是个
一次性的命令式操作,不适合声明式建模。

DebianForm 的做法是:把 **armored 公钥本身**当成一个被管理的文件
(`debian_file`)。现代 Debian(trixie)的 APT 允许 `Signed-By` 直接指向
`.asc`(armored)文件,因此完全不需要 dearmor 这一步。这样公钥就和其他文件
一样:有明确的期望内容、可比对、可修复漂移。

## 使用步骤

1. 把示例里的 host 别名 `bird_host` 换成你的 SSH 主机(可写在 `~/.ssh/config`
   的 `Host`)。DebianForm 始终以 `root` 登录。

2. 先把 CZ.NIC 公钥下载到本地(只需一次,`file()` 会在 `plan/apply` 时读取它):

   ```bash
   mkdir -p examples/keys
   curl -fsSL https://pkg.labs.nic.cz/gpg -o examples/keys/cznic.asc
   ```

3. 预览与应用:

   ```bash
   dbf plan  -f examples/bird2.dbf.hcl
   dbf apply -f examples/bird2.dbf.hcl
   ```

4. 验证(应当无变更,即已收敛):

   ```bash
   dbf check -f examples/bird2.dbf.hcl
   ```

## 顺序与依赖

资源之间用 `depends_on` 表达顺序,`dbf` 按依赖拓扑排序后执行:

```
debian_directory.keyrings
        └── debian_file.cznic_key
                    └── debian_apt_source.cznic_bird2
                                    └── debian_package.bird2 ── debian_service.bird
debian_package.ca_certificates ─────────────┘
```

`debian_package.bird2` 设置了 `update_cache = true`,会在安装前执行
`apt-get update`,从而读到刚写入的 CZ.NIC 仓库。

## 调整为其他系统

仓库的 `suites` 用 `local.suite` 控制(示例为 `trixie`)。换成目标系统的
codename(例如 `bookworm`)即可:

```hcl
locals {
  suite = "bookworm"
}
```
