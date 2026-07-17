# DebianForm 删除行为提示设计稿

<p align="right"><a href="delete-behavior-diagnostics-plan.md">English</a> | <strong>简体中文</strong></p>

本文档记录删除行为提示的需求讨论。结论：弃用两阶段删除方案，不实现
`prepare-destroy` / `prepare-remove`。更合适的方向是在 `plan` 和 `apply` 中把删除动作讲清楚：
系统会删除什么、保留什么、是否恢复运行时状态、是否可能影响用户数据。

## 背景

DebianForm 接管的是已有 Debian 系统，不是 NixOS 式从零重建。删除 `.dbf.hcl` 中的配置时，用户很容易
误以为系统会恢复到接管前状态；但对大多数资源，当前真实语义更接近：

- 删除 DebianForm 写入的持久化产物。
- 从远端 state 中移除资源记录。
- 对 adopted 或共享资源，只解除管理，不一定恢复、不一定销毁。

因此不应该把“自动恢复”做成通用功能。更可控的做法是：在 plan/apply 中用更明确的风险标注和说明，
让用户在执行前知道删除会发生什么。

## 目标

- 在 `plan` 和 `apply` 的删除项中展示删除行为分类。
- 用颜色标注删除动作的风险和恢复语义。
- 在 `plan` 和 `apply` 输出底部显示提示文档链接，说明颜色含义和删除语义。
- 在 JSON/HTML plan 中保留机器可读的删除行为诊断信息。
- 用矩阵列出每个 resource/provider 类型在删除时会执行什么操作。
- 用矩阵列出默认删除行为能否修改、如何修改，以及哪些 resource 不支持自定义删除行为。

## 非目标

- 不实现 `prepare-destroy` / `prepare-remove`。
- 不在删除时猜系统默认值。
- 不默认恢复接管前状态。
- 不把所有 resource 都强行改成 restore 模型。
- 不把颜色作为唯一信息；文本和 JSON 字段必须表达同样语义，方便无颜色终端和 CI 使用。

## 删除提示模型

建议为每个 delete/destroy/forget 动作增加一个删除行为分类：

| 分类 | 建议颜色 | 含义 | 用户预期 |
| --- | --- | --- | --- |
| `forget` | 灰色 | 只从 DebianForm state 中解除管理，不修改远端资源。 | 远端系统保持现状。 |
| `remove-managed-artifact` | 黄色 | 删除 DebianForm 写入的持久化文件或管理产物。 | 不保证恢复运行时状态。 |
| `restore-original` | 蓝色 | 尝试恢复 state 中保存的接管前内容。 | 只适用于显式支持 restore 的资源。 |
| `destructive` | 红色 | 可能删除用户数据、卸载包、删除账号、停止项目或递归删除目录。 | 执行前必须特别审阅。 |
| `external-side-effect` | 紫色 | 删除会触发额外 operation，例如 reload/restart/update。 | 需要检查服务影响窗口。 |
| `unknown` | 高亮黄色 | provider 无法明确说明删除后果。 | 应保守处理，建议手动确认。 |

文本输出中，删除项除了现有 summary，还应补充一行说明，例如：

```text
- host.bbr1.kernel.sysctl["net.ipv4.tcp_congestion_control"]
  remove sysctl net.ipv4.tcp_congestion_control
  delete behavior: remove-managed-artifact
  note: removes /etc/sysctl.d/99-dbf-net_ipv4_tcp_congestion_control.conf; runtime value is not restored
```

`plan` 和 `apply` 输出底部应显示统一提示，例如：

```text
Delete behavior legend: grey=forget, yellow=remove managed artifact, blue=restore original,
red=destructive, purple=external side effect. See docs/delete-behavior-diagnostics-plan.zh.md.
```

## 输出格式要求

文本 plan：

- 对 delete/destroy/forget 项展示分类和说明。
- 只有存在删除类动作时，底部显示颜色说明和文档路径。
- 无颜色或 `NO_COLOR` 环境下仍输出分类文本。

JSON plan：

- 建议在 `changes[]` 中增加字段：
  - `delete_behavior`
  - `delete_notes`
  - `delete_risk`
- 字段只在 delete/destroy/forget 类动作中出现。

HTML plan：

- 使用同一分类渲染颜色 badge。
- 页面底部展示颜色图例和文档链接。

Apply：

- `apply` 已经会先打印计划，执行后再打印实际执行计划。
- 两处都应保留删除行为说明。
- 如果执行后的实际计划没有删除项，则不显示删除图例。

## 如何修改默认删除行为

删除行为必须以“显式配置”为准，不能靠 DebianForm 猜测用户意图。用户想改变默认删除行为时，
应该通过 resource 已支持的 DSL 字段表达；没有字段支持时，`plan` / `apply` 只能提示当前行为和限制，
不能悄悄替用户恢复、销毁或保留远端资源。

### 基本规则

- 从 `.dbf.hcl` 中删除一段配置，表示“不再用这段配置声明目标状态”。具体会 destroy、forget、restore
  还是触发 operation，取决于该 resource 的 provider 语义。
- 在 `.dbf.hcl` 中保留 resource，并设置 `ensure = "absent"`、`state = "absent"`、
  `enabled = false`、`state = "stopped"` 等字段，表示用户显式要求远端收敛到某个状态。
- `lifecycle.prevent_destroy = true` 是保护开关，不是恢复策略。它应阻止删除、替换或显式 absent
  产生的危险动作，让用户先修改配置或手动处理。
- `on_destroy` 是 provider 专属字段。只有明确支持的 resource 可以使用，不能作为所有 resource 的通用字段。
- `remove_orphans`、`remove_conflicts`、`validate`、`activate` 这类字段只控制对应 provider 的附加行为，
  不代表 resource 删除后可以恢复接管前状态。
- 如果某个 resource 不支持自定义删除行为，文档和 plan 应明确写出“不支持自定义”，并说明可以采用的安全做法。

### 常见修改方式

| 用户意图 | 推荐写法 | 适用范围 | 说明 |
| --- | --- | --- | --- |
| 删除前先收敛到一个已知值 | 保留 resource，把字段改成目标值，先 `plan/apply`；确认系统符合预期后，再考虑删除配置。 | `sysctl`、`service`、`docker.compose` 等有目标状态字段的 resource。 | 这不是自动恢复接管前状态，而是用户显式声明新的目标状态。 |
| 删除配置但保留远端产物 | 使用 provider 支持的 keep/forget 语义，例如 `apt.source_file.on_destroy = "keep"`。 | 仅明确支持 keep/forget 的 resource。 | 不支持时不能通过通用字段强行 keep。 |
| 删除配置并恢复接管前内容 | 使用 provider 支持的 restore 语义，例如 `apt.source_file.on_destroy = "restore"`。 | 仅 state 中保存了原始内容且 provider 支持 restore 的 resource。 | restore 不能推广到 secret、普通 file、directory、package、user 等资源。 |
| 明确要求远端对象不存在 | 使用 `ensure = "absent"` 或 `state = "absent"`。 | 支持 ensure/state 的 resource。 | 这比“从配置删除”更强，应该在 plan 中显示 destructive 或 external side effect 风险。 |
| 阻止误删 | 加 `lifecycle { prevent_destroy = true }`。 | 支持 lifecycle 的 resource。 | 适合 package、user、directory、file、systemd unit、nftables file 等高风险资源。 |
| 控制删除附带清理 | 使用 provider 专属字段，例如 `docker.compose.remove_orphans`。 | Docker Compose 等特定 provider。 | 只改变附加清理范围，不等于恢复旧状态。 |
| 避免删除时触发 reload/activate | 修改 provider 的触发字段，例如 `nftables.validate`、`nftables.activate`。 | 支持 validate/activate 的 resource。 | 需要在矩阵里说明哪些 operation 会被影响。 |

### BBR / sysctl 示例

以 BBR 为例，如果用户删除 `kernel.sysctl["net.ipv4.tcp_congestion_control"]` 配置，默认行为只应删除
DebianForm 写入的 `/etc/sysctl.d/99-dbf-...conf`，不应该猜测并恢复运行时值。

如果用户希望恢复到 `cubic`，应先显式写入目标值：

```hcl
host "server1" {
  kernel {
    sysctl "net.ipv4.tcp_congestion_control" {
      value = "cubic"
    }
  }
}
```

执行 `plan/apply` 后，系统会收敛到用户声明的值。之后如果再删除这段配置，删除行为仍然只是清理
DebianForm 管理的持久化文件，不应被解释为“恢复默认值”。

## 删除行为矩阵

下面矩阵描述当前或建议的删除行为。它应成为后续实现和测试的来源。

| Resource / Provider 类型 | 来源 DSL | 默认删除时远端操作 | 默认是否恢复接管前状态 | 当前可自定义行为 | 不支持/限制 | 建议分类 |
| --- | --- | --- | --- | --- | --- | --- |
| `sysctl` | `kernel.sysctl` | 删除 `/etc/sysctl.d/99-dbf-<key>.conf`；从 state 移除。 | 否 | 当前无删除行为字段；可通过保留配置并把值改成期望恢复值来显式收敛。 | 不能自动恢复运行时值；不能猜默认值。 | `remove-managed-artifact` |
| `kernel_module` | `kernel.modules` | 删除 `/etc/modules-load.d/dbf-<module>.conf`；尝试 `modprobe -r`。 | 否 | 当前无删除行为字段。 | 不能声明“只删持久化但不 unload”；模块是否可卸载取决于内核和依赖。 | `external-side-effect` |
| `package` | `packages.install`、Docker packages | `apt-get remove -y <package>`，或 adopted 时 forget。 | 否 | 通过 adopted/managed ownership 间接影响；Docker conflict 可用 `remove_conflicts`。 | 普通 package 目前没有 `on_destroy = keep/remove` 这类显式字段。 | `destructive` |
| `docker_package_conflicts` | `docker.package.remove_conflicts` | 删除冲突包；删除配置后通常无恢复动作。 | 否 | `remove_conflicts = "auto" / true / false` 控制是否移除冲突包。 | 不支持把冲突包装回去。 | `destructive` |
| `apt_signing_key` | `apt.repository.signing_key` | 删除 keyring 文件。 | 否 | 当前无删除行为字段。 | 不能恢复旧 keyring 内容。 | `remove-managed-artifact` |
| `apt_source_file` | `apt.source_file` | `on_destroy = keep` 时 forget；`restore` 时恢复原内容或删除新建文件。 | 仅 `restore` | `on_destroy = "keep"` 或 `"restore"`。 | 只支持 source file；不代表所有 file 都有 restore。 | `forget` 或 `restore-original` |
| `file` | `files.file`、Docker daemon/Compose 文件 | 删除目标文件。 | 否 | 当前普通 `files.file` 无删除行为字段；`lifecycle.prevent_destroy` 可阻止删除。 | 不支持 restore 原内容；write-only/sensitive 内容不能反推恢复。 | `destructive` 或 `remove-managed-artifact` |
| `secret` | `secrets.file` | 删除目标 secret 文件。 | 否 | `lifecycle.prevent_destroy` 可阻止删除。 | 不支持保存或恢复原 secret 原文。 | `destructive` |
| `directory` | `directories.directory`、Compose directory | 当前可能递归删除目录。 | 否 | `ensure = "absent"` 可显式要求缺失；`lifecycle.prevent_destroy` 可阻止删除。 | 不支持“只 forget 不删目录”的字段；目录内容无法安全恢复。 | `destructive` |
| `group` | `groups.group`、Docker group | 删除 group。 | 否 | `ensure = "absent"` 可显式要求缺失；`lifecycle.prevent_destroy` 可阻止删除。 | 不支持恢复原 membership；删除系统组风险高。 | `destructive` |
| `user` | `users.user` | 删除 user。 | 否 | `ensure = "absent"` 可显式要求缺失；`lifecycle.prevent_destroy` 可阻止删除。 | home/spool 删除策略需要确认；不支持恢复原账号状态。 | `destructive` |
| `user_group_membership` | `docker.users` | 从 supplementary group 中移除用户。 | 否 | 当前由 `docker.users` 列表间接控制。 | 不支持 per-user membership 删除策略；已登录会话不立即生效。 | `external-side-effect` |
| `ssh_authorized_key` | `users.user.authorized_keys` | 删除 authorized key 行。 | 否 | 通过 key 列表控制；`lifecycle.prevent_destroy` 对所属 user 资源有保护边界。 | 不支持恢复被覆盖/删除前的 key 集合。 | `destructive` |
| `systemd_unit` | `systemd.unit`、`systemd.service_unit`、Compose unit | 删除 unit 文件并 daemon-reload。 | 否 | `lifecycle.prevent_destroy` 可阻止删除；service 状态另由 `services.service` 控制。 | 不支持恢复接管前 unit 内容。 | `external-side-effect` |
| `service` | `services.service`、Docker service、Compose service | 删除配置时通常 forget 或不直接修改服务；显式 desired 可 stop/disable。 | 否 | 通过 `enabled` 和 `state` 设置期望状态。 | 删除配置不应被理解为 stop/disable；当前行为需要实现核对。 | `forget` 或 `external-side-effect` |
| `nftables_file` | `nftables.file` | 删除 nftables 文件并触发 validate/activate。 | 否 | `validate`、`activate` 控制规则变更后的操作；`lifecycle.prevent_destroy` 可阻止删除。 | 不支持恢复旧规则内容。 | `external-side-effect` |
| `component_download` | `component.source` | 删除下载缓存或源文件。 | 否 | 组件声明间接控制；通常无需自定义。 | 不支持 restore。 | `remove-managed-artifact` |
| `component_build` | `component.source build` | 删除 build output。 | 否 | 组件声明间接控制；可重新 build。 | 不支持 restore。 | `remove-managed-artifact` |
| `component_binary` | `component.install binary` | 删除安装到目标路径的 binary。 | 否 | `lifecycle.prevent_destroy` 可阻止删除。 | 不支持恢复旧 binary。 | `destructive` |
| `component_file` | `component.install file` | 删除安装文件。 | 否 | `lifecycle.prevent_destroy` 可阻止删除。 | 不支持恢复旧文件。 | `destructive` |
| `component_archive` | `component.install archive` | 删除目标目录。 | 否 | `lifecycle.prevent_destroy` 可阻止删除。 | 不支持恢复目录内容。 | `destructive` |
| `component_ca_certificate` | component CA certificate | 删除证书文件并触发 `update-ca-certificates`。 | 否 | 组件声明间接控制。 | 不支持恢复旧证书；会影响系统信任链。 | `external-side-effect` |
| `docker_compose_project` | `docker.compose` | `docker compose down` 或停止/移除 project。 | 否 | `state = "running"`、`"stopped"` 或 `"absent"`，`remove_orphans` 控制 orphan 清理。 | volume 删除行为必须保持明确；不支持恢复原 Compose project 状态。 | `destructive` |
| `operation` | graph operations | 不是资源本体；由资源变化触发。 | 不适用 | 通过上游资源字段间接控制，例如 reload/activate/pull。 | 不能单独自定义删除行为。 | `external-side-effect` |

## 颜色与可访问性

- 默认仅在 TTY 中启用颜色。
- 支持 `NO_COLOR` 禁用颜色。
- JSON 输出不包含 ANSI color，只包含分类字段。
- 文本必须同时显示分类名，不能只靠颜色传达语义。
- 红色只用于真正 destructive 或可能影响用户数据/服务可用性的动作。
- 通用颜色、日志颜色和 CI/JSON 边界见 `docs/cli-color-output-policy.zh.md`。

## 可验证实现 Loop

### Loop 1：删除行为数据模型（已实现）

范围：

- 在 plan change 数据中增加删除行为字段。
- 定义分类枚举和风险级别。
- provider/engine 不拼接颜色，只返回或推导机器可读分类和说明。

验收：

- JSON plan 的删除项可以读到 `delete_behavior`、`delete_notes`、`delete_risk`。
- create/update/no-op/run 不输出删除行为字段。
- plan format 的新增字段符合兼容性政策。

### Loop 2：核心 provider 分类（已实现，仍需持续核对矩阵）

范围：

- 优先覆盖高频和高风险资源：`sysctl`、`apt_source_file`、`file`、`secret`、`directory`、
  `package`、`user`、`group`、`systemd_unit`、`nftables_file`、`docker_compose_project`。
- 对 adopted、keep、shared directory 等 forget 情况给出 `forget` 分类。

验收：

- BBR/sysctl 删除提示为 `remove-managed-artifact`。
- apt source file `on_destroy = "keep"` 为 `forget`，`restore` 为 `restore-original`。
- directory/package/user/group/docker compose project 为 `destructive`。
- systemd/nftables 等带 reload/activate 的删除为 `external-side-effect`。

### Loop 3：文本 plan/apply 提示（已实现）

范围：

- 在删除项下方输出 `delete behavior` 和 `note`。
- 有删除项时在底部输出颜色图例和文档路径。
- `NO_COLOR` 或非 TTY 下仍输出完整文本。

验收：

- 文本 plan 中删除项不需要看颜色也能理解行为。
- 无删除项时不显示图例。
- `apply` 执行前计划和执行后计划都保留删除提示。

### Loop 4：HTML plan 和文档同步（部分已实现）

范围：

- HTML plan 渲染删除行为 badge 和图例。
- 同步 `docs/plan-format.md`、CLI 文档和 how-it-works。
- 补 provider 矩阵与实际实现差异。

验收：

- HTML 中删除行为可筛选或至少可见。
- plan JSON 文档列出新增字段和兼容性说明。
- 删除行为矩阵不再有“需要实现核对”的核心项。

当前状态：

- HTML badge 和图例已实现。
- `docs/plan-format.md`、CLI 文档和 how-it-works 已同步。
- provider 矩阵仍需要结合真实 provider 行为逐项核对，特别是 `service` 和目录删除默认策略。

## 验收标准

- BBR 删除 plan 明确提示 sysctl 删除只清理持久化文件，不恢复运行时值。
- apt source file 的 `keep` 与 `restore` 在 plan 中展示为不同分类。
- 删除 directory、package、user、docker compose project 时显示 destructive 标注。
- 删除 systemd/nftables/CA certificate 等会触发 operation 的资源时显示 external side effect 标注。
- plan/apply 底部只在存在删除动作时展示颜色说明和文档链接。
- `--format json` 中删除行为信息可被 CI 读取，不依赖颜色。

## 待定问题

- 当前 provider 中哪些删除行为与上表不一致，需要先核对实现。
- `service` 删除配置时到底应该 forget、stop、disable，还是维持当前行为并提示。
- `directory` 是否应默认从 destructive 改为更保守的 forget 或 require explicit absent。
