# Libvirt 集成测试

<p align="right"><a href="README.md">English</a> | <strong>简体中文</strong></p>

这些测试使用 libvirt 启动全新的 Debian 12、Debian 13、Ubuntu 24.04 或 Ubuntu 26.04 amd64
cloud VM，并针对真实远端 state 执行完整的受管目标生命周期。Debian 13 是默认和主要目标。
Debian 12、Debian 13 和 Ubuntu 24.04 分别具有阻塞 CI 矩阵；Ubuntu 的初始支持层级为 Preview。
Ubuntu 26.04 目标可用于 #53 实现基线，但在其独立阻塞矩阵全绿前不构成支持声明。

在默认 Debian 13 目标上运行所有 case，或显式选择任一支持版本：

```bash
make test-integration
make test-integration DEBIAN_VERSION=12
make test-integration DEBIAN_VERSION=13
make test-integration TARGET=ubuntu-24.04
make test-integration TARGET=ubuntu-26.04
```

运行单个 case：

```bash
make test-integration-case CASE=apt-source
make test-integration-case CASE=apt-source DEBIAN_VERSION=12
make test-integration-case CASE=files TARGET=ubuntu-24.04
make test-integration-case CASE=files TARGET=ubuntu-26.04
make test-integration-case CASE=bbr
make test-integration-case CASE=component-inputs
make test-integration-case CASE=files
make test-integration-case CASE=multi-directory
make test-integration-case CASE=nftables
make test-integration-case CASE=shadowsocks-rust
make test-integration-case CASE=script-on-change
make test-integration-case CASE=systemd-service-unit
make test-integration-case CASE=wireguard
```

`TARGET` 接受 `debian-12`、`debian-13`、`ubuntu-24.04` 或 `ubuntu-26.04`。省略 `TARGET`
时，兼容变量 `DEBIAN_VERSION=12|13` 选择 Debian，默认值为 Debian 13。目标描述符会解析官方
cloud image，验证其 SHA256 或 SHA512 发布方 checksum，并在执行 case 前断言 guest 的 ID、
version、codename 和 architecture。

每个编号 case 步骤都会运行 `validate`、在线 JSON plan；在 case 提供 drift hook 时拒绝 drift；
然后运行 `apply`、断言为 no-op 的 JSON plan、`check` 以及 case 特有的 apply 后检查。CI 当前发现
20 个 case 目录。未改变的 Debian 矩阵展开为 2 个版本 x 20 个 case，即 40 个由
`Managed target matrix gate` 守卫的 job。独立 Ubuntu 矩阵运行相同的 20 个 case，并由
`Ubuntu 24.04 target matrix gate` 守卫。

`wireguard` 使用双 host runner，`wireguard-three-host` 使用三 host runner。在 Ubuntu 上，
这些 case 和 `shared-script-networkd` 带有 `native-networkd.case` 标记。fixture 会禁用未来的
cloud-init 网络渲染，移除 Netplan ownership，安装持久的 networkd uplink，并在 DebianForm
运行前重启。此 fixture 准备过程不是 DebianForm 的 Netplan 能力。

不启动 VM 也可验证 case 布局：

```bash
make test-integration-layout
```

实用环境变量：

| 变量 | 用途 |
| --- | --- |
| `TARGET` | 公开目标选择器：`debian-12`、`debian-13`、`ubuntu-24.04` 或 `ubuntu-26.04`。 |
| `DEBIAN_VERSION` | 选择 `12` 或 `13` 的公开 Make 变量；默认 `13`。 |
| `DBF_INTEGRATION_TARGET` | CI 和直接调用脚本使用的内部通用目标选择器。 |
| `DBF_INTEGRATION_CASE` | 运行单个 case 目录。 |
| `DBF_INTEGRATION_WORKDIR` | 覆盖临时工作目录。 |
| `DBF_INTEGRATION_KEEP_WORKDIR=1` | 运行后保留工作目录。 |
| `DBF_INTEGRATION_ARTIFACT_DIR` | 失败诊断产物目录。 |
| `DBF_INTEGRATION_IMAGE_CACHE` | 所选 cloud image 的缓存目录。 |
| `DBF_INTEGRATION_DISABLE_KVM=1` | 强制使用 QEMU 软件模拟。 |
| `DBF_LIBVIRT_URI` / `LIBVIRT_DEFAULT_URI` | Libvirt URI。单、双、三 host case 可在 hypervisor 上创建 VM 产物，从而支持远端 `qemu+ssh://...`。 |
| `DBF_INTEGRATION_HYPERVISOR` | 无法从 URI 推断时，用于远端 libvirt 文件系统的 SSH host。 |
| `DBF_INTEGRATION_POOL` | 远端 VM 产物使用的 libvirt storage pool，默认 `vm`。 |
| `DBF_INTEGRATION_REMOTE_BASE_IMAGE` | Hypervisor 侧的基础镜像路径。其摘要必须匹配已验证的官方镜像；陈旧或损坏的副本会被原子替换。 |
