# DebianForm v2 Docker / Docker Compose 实施计划

本文档把 `docs/v2-docker-compose-requirements.zh.md` 拆成可实现、可检验的开发 loop。
每个 loop 都必须形成一个可合并闭环：

- [ ] 代码路径可运行
- [ ] parser / merge / HostSpec / ResourceGraph / plan 中受影响层级有测试或 golden
- [ ] apply / check 语义有 fake runner 或 provider 单测
- [ ] 至少一个示例进入验收输入
- [ ] 文档同步更新
- [ ] `make test` 通过

状态约定：

- [x] 已完成
- [ ] 未完成

## 当前基线

- [x] Docker / Docker Compose 需求文档已保存：`docs/v2-docker-compose-requirements.zh.md`
- [x] v2 已有 parser、merge、HostSpec、ResourceGraph、plan、apply、check 主流程
- [x] v2 已有 APT repository、APT signing key、package、file、directory、group、systemd unit、service 和 operation provider
- [x] v2 已有 runtime facts discovery，可在线探测 `system.architecture` 和 `system.codename`
- [x] v2 已有 fake runner / memory provider 测试基础
- [ ] 尚未支持顶层 `docker` DSL
- [ ] 尚未支持 Docker daemon JSON 高阶配置
- [ ] 尚未支持 Docker Compose project 高阶对象
- [ ] 尚未支持 Compose project 状态漂移检测

## 总体实现边界

MVP 完成线：

- `docker { enable = true }` 默认使用 Docker 官方 APT 源
- 官方 Docker packages 安装和 `docker.service` enable/start
- `/etc/docker/daemon.json` 管理和变更后 restart
- Compose directory / compose file / env file 写入
- `docker compose config` 校验
- Compose project `running` / `stopped` / `absent`
- Compose systemd unit 生成和 enable/start
- plan / apply / check / drift 基本闭环

MVP 不包含：

- HCL 生成 Compose YAML
- container / image / network / volume 低阶资源
- registry login
- rootless Docker
- Swarm / Kubernetes
- Podman backend
- 私有 registry 完整生命周期

## 全局设计约束

- 用户只写高阶 `docker` / `docker.compose` DSL，不直接声明 Docker 低阶资源。
- ResourceGraph 可以复用现有低阶 provider，但 plan 地址应保留 Docker 领域语义，例如
  `host.server1.docker.package["docker-ce"]`，debug 模式再暴露 provider address。
- `docker { enable = false }` 表示 DebianForm 不管理 Docker，默认不卸载、不停服务。
- Docker 官方 APT 源依赖目标主机 `architecture` 和 `codename`。在线 `plan/apply/check`
  使用 runtime facts；`plan --offline` 必须要求用户显式声明匹配的 system facts，否则清晰失败。
- `env_file` 默认按敏感文件处理，plan/state/log 只输出摘要，避免 `.env` 内容泄露。
- 单元测试和 golden 测试不得依赖真实 Docker 网络下载；真实 Docker 安装和 Compose 运行放到 libvirt 集成用例。

## Loop 1: Docker DSL 解析、合并与 HostSpec

目标：让 `docker` block 可以通过 `validate`，形成稳定 HostSpec，但暂不生成 ResourceGraph。

范围：

- 顶层 `docker`
- `docker.enable`
- `docker.package`
- `docker.service`
- `docker.daemon.settings`
- `docker.users`
- `docker.compose "<name>"`
- `compose.file`
- `compose.env_file "<name>"`

代码：

- [ ] parser 支持 host/profile 内的 `docker` block
- [ ] parser 支持 `compose "<name>"` 和 `env_file "<name>"` labeled block
- [ ] IR 增加 `DockerSpec`、`DockerPackageSpec`、`DockerServiceSpec`、`DockerDaemonSpec`
- [ ] IR 增加 `DockerComposeSpec`、`DockerComposeFileSpec`、`DockerComposeEnvFileSpec`
- [ ] merge 支持 `docker` block 在 profile 和 host 之间合并
- [ ] merge 支持多个 `compose` project 按 label 合并
- [ ] merge 支持多个 `env_file` 按 label 合并
- [ ] 默认填充 `package.source = "official"`、`package.channel = "stable"`
- [ ] 默认填充 `service.enable = true`、`service.state = "running"`
- [ ] 默认填充 `compose.enable = true`、`compose.state = "running"`
- [ ] 默认填充 `compose.project = <compose label>`
- [ ] 默认填充 compose file owner/group/mode 为 `root/root/0644`
- [ ] 默认填充 env file owner/group/mode 为 `root/root/0600`
- [ ] `daemon.settings` 保留为 JSON-compatible map/list/scalar，并拒绝不能序列化为 JSON 的值
- [ ] 校验 `package.source` 只能是 `official`、`debian`、`none`、`custom`
- [ ] 校验 `package.remove_conflicts` 只能是 `auto`、`true`、`false`
- [ ] 校验 service state 只能是 `running`、`stopped`
- [ ] 校验 compose state 只能是 `running`、`stopped`、`absent`
- [ ] 校验 compose `pull` 只能是 `never`、`missing`、`always`
- [ ] 校验 compose `recreate` 只能是 `auto`、`always`、`never`
- [ ] 校验 compose `directory` 在启用 compose project 时必填
- [ ] 校验 compose `file.path` 和 `file.content/source` 语义
- [ ] 校验 compose project label、project name、systemd service name 非空且稳定

测试：

- [ ] parser 单测覆盖最小 `docker { enable = true }`
- [ ] parser 单测覆盖 daemon nested map/list settings
- [ ] parser 单测覆盖 compose file、inline YAML、多个 env_file
- [ ] merge 单测覆盖 profile 默认 docker 配置被 host 覆盖
- [ ] HostSpec golden 覆盖 `v2-docker-minimal`
- [ ] HostSpec golden 覆盖 `v2-docker-daemon`
- [ ] HostSpec golden 覆盖 `v2-docker-compose`
- [ ] 负例覆盖非法 enum、重复 compose label、缺失 directory、非法 file content/source 组合

示例：

- [ ] 新增 `examples/v2-docker-minimal.dbf.hcl`
- [ ] 新增 `examples/v2-docker-daemon.dbf.hcl`
- [ ] 新增 `examples/v2-docker-compose.dbf.hcl`

文档：

- [ ] README 增加 Docker 最小语法预览，并标明当前 loop 只完成 validate

验收：

```bash
dbf validate -f examples/v2-docker-minimal.dbf.hcl
dbf validate -f examples/v2-docker-daemon.dbf.hcl
dbf validate -f examples/v2-docker-compose.dbf.hcl
make test
```

## Loop 2: `docker.enable` 官方源 ResourceGraph 与 plan

目标：把 `docker { enable = true }` 编译为官方 Docker APT 源、官方 packages 和
`docker.service`，并能输出稳定 plan。

范围：

- 只实现 `package.source = "official"`
- 只实现默认 package 列表
- 只实现 `docker.service`
- 暂不实现 daemon、users、compose
- 暂不实现 `source = "debian" | "none" | "custom"`
- 暂不实现冲突包删除

代码：

- [ ] ResourceGraph 编译 Docker 官方 APT signing key 节点
- [ ] ResourceGraph 编译 Docker 官方 APT repository 节点
- [ ] ResourceGraph 编译 host-scoped APT cache refresh operation
- [ ] ResourceGraph 编译默认 packages：
  - `docker-ce`
  - `docker-ce-cli`
  - `containerd.io`
  - `docker-buildx-plugin`
  - `docker-compose-plugin`
- [ ] ResourceGraph 编译 `docker.service` service 节点
- [ ] 官方 repository 使用目标 host 的 `system.codename`
- [ ] 官方 repository 使用目标 host 的 `system.architecture`
- [ ] 在线 plan 使用 runtime facts 后重新编译 Docker repository
- [ ] `plan --offline` 缺少 architecture/codename 时给出清晰错误
- [ ] Docker 领域节点使用高阶地址，provider payload 复用现有 APT/package/service provider
- [ ] 推导依赖：signing key -> repository -> apt cache refresh -> packages -> service
- [ ] `enable = false` 不生成任何 Docker 节点

测试：

- [ ] ResourceGraph golden 覆盖官方 signing key、repository、packages、service 地址
- [ ] ResourceGraph 单测覆盖依赖顺序
- [ ] plan JSON golden 覆盖 Docker 最小 create plan
- [ ] plan text golden 覆盖高阶 Docker 地址
- [ ] 负例覆盖 offline 缺失 runtime facts
- [ ] 负例覆盖 `package.source = "debian"` 在本轮返回 not implemented 诊断

示例：

- [ ] `examples/v2-docker-minimal.dbf.hcl` 进入 graph / plan golden
- [ ] 示例中为 offline golden 显式声明 `system.architecture` 和 `system.codename`

文档：

- [ ] README 标明 `docker { enable = true }` 已可 plan
- [ ] Docker 需求文档旁补充实现状态链接到本文档

验收：

```bash
dbf plan -f examples/v2-docker-minimal.dbf.hcl --offline
dbf plan -f examples/v2-docker-minimal.dbf.hcl --offline --format json
make test
```

## Loop 3: Docker Engine apply / check 闭环

目标：让 Loop 2 生成的 Docker Engine 资源可以通过现有 provider apply/check，并保持幂等。

代码：

- [ ] 确认 Docker official signing key 节点能走现有 `apt_signing_key` provider
- [ ] 确认 Docker repository 节点能走现有 file / APT source provider
- [ ] 确认 Docker package 节点能走现有 package provider
- [ ] 确认 `docker.service` 能走现有 service provider
- [ ] 如果高阶 Docker 地址影响 state，补齐 state ownership / provider address 测试
- [ ] apply 后 state 记录 Docker 高阶地址和低阶 provider address
- [ ] check 能检测 package 缺失、service disabled、service stopped

测试：

- [ ] NativeProvider fake runner 测试 Docker signing key apply 脚本
- [ ] NativeProvider fake runner 测试 Docker packages apply 命令
- [ ] NativeProvider fake runner 测试 Docker service enable/start 命令
- [ ] Engine fake apply 后立即 plan 为 no-op
- [ ] check 返回码测试覆盖 Docker service drift

示例：

- [ ] `examples/v2-docker-minimal.dbf.hcl` 进入 fake runner apply 测试

文档：

- [ ] README 标明 Docker Engine 已可 apply/check

验收：

```bash
dbf plan -f examples/v2-docker-minimal.dbf.hcl --offline
make test
```

## Loop 4: Docker daemon 配置

目标：支持 `docker.daemon.settings` 管理 `/etc/docker/daemon.json`，并在配置变化后 restart Docker。

代码：

- [ ] 将 `daemon.settings` deterministic JSON 序列化为 `/etc/docker/daemon.json`
- [ ] 编译 Docker daemon file 节点，默认 owner/group/mode 为 `root/root/0644`
- [ ] daemon file 依赖 Docker package 安装
- [ ] `docker.service` 依赖 daemon file，保证首次启动前配置已写入
- [ ] 编译 `docker.daemon.restart` operation
- [ ] restart operation 由 daemon file 变化触发
- [ ] restart operation 依赖 daemon file 和 `docker.service`
- [ ] `package.source = "none"` 时仍允许管理 daemon file 和 service
- [ ] drift 检测显示 daemon JSON 内容 diff 或摘要

测试：

- [ ] HostSpec golden 覆盖 nested daemon settings
- [ ] ResourceGraph golden 覆盖 daemon file 和 restart operation
- [ ] plan text golden 覆盖 daemon JSON 行级 diff
- [ ] NativeProvider fake runner 测试 daemon file 写入
- [ ] Engine fake apply 后修改 daemon desired，plan 包含 restart operation
- [ ] 负例覆盖 daemon settings 中不能 JSON 序列化的值

示例：

- [ ] `examples/v2-docker-daemon.dbf.hcl` 进入 golden

文档：

- [ ] README 增加 daemon settings 示例
- [ ] 文档明确 MVP 统一使用 restart，不做细粒度 reload

验收：

```bash
dbf plan -f examples/v2-docker-daemon.dbf.hcl --offline
dbf plan -f examples/v2-docker-daemon.dbf.hcl --offline --format json
make test
```

## Loop 5: Compose 文件、env 文件与配置校验

目标：支持 Compose project 的目录、compose 文件、env 文件写入，并在 apply 前执行
`docker compose config` 校验。

范围：

- `compose "<name>"`
- `directory`
- `file`
- 多个 `env_file`
- `docker compose config`
- 暂不实现 project up/stop/down
- 暂不生成 systemd unit

代码：

- [ ] 编译 compose working directory 节点，默认 owner/group/mode 为 `root/root/0755`
- [ ] 编译 compose file 节点，默认 owner/group/mode 为 `root/root/0644`
- [ ] 编译 env file 节点，默认 owner/group/mode 为 `root/root/0600`
- [ ] env file 默认 `sensitive = true` 或 `content_write_only = true`
- [ ] compose file / env file 依赖 working directory
- [ ] compose file / env file 依赖 Docker Engine packages/service，除非 `package.source = "none"`
- [ ] 编译 `docker.compose["<name>"].validate` operation
- [ ] validate operation command preview 使用 `docker compose -p <project> -f <file> config`
- [ ] validate operation 依赖 compose file 和 env files
- [ ] 后续所有 project 状态和 systemd 节点依赖 validate operation
- [ ] 校验同一 host 下 compose file/env file path 不与 files/secrets/nftables/networkd/systemd unit 冲突

测试：

- [ ] ResourceGraph golden 覆盖 directory、compose file、env file、validate operation
- [ ] plan golden 覆盖 compose YAML 行级 diff
- [ ] sensitive 测试确保 env file 内容不进入 plan/state/log
- [ ] operation 单测覆盖 validate command preview
- [ ] 负例覆盖 path 冲突、env_file label 重复、compose file 缺失

示例：

- [ ] `examples/v2-docker-compose.dbf.hcl` 进入 graph / plan golden

文档：

- [ ] README 增加 Compose file 管理示例
- [ ] 文档明确 DebianForm 不解析、不重写 Compose schema

验收：

```bash
dbf plan -f examples/v2-docker-compose.dbf.hcl --offline
dbf plan -f examples/v2-docker-compose.dbf.hcl --offline --format json
make test
```

## Loop 6: Compose project 状态 provider

目标：支持 Compose project `running`、`stopped`、`absent` 的 plan/apply/check。

代码：

- [ ] 新增 provider kind `docker_compose_project`
- [ ] ResourceGraph 编译 `host.<host>.docker.compose["<name>"].project` 节点
- [ ] project 节点 desired 包含 `directory`、`project`、compose files、env files、`state`
- [ ] project 节点 desired 包含 `pull`、`recreate`、`remove_orphans`
- [ ] project 节点依赖 validate operation
- [ ] project 节点依赖 Docker Engine service，除非 `package.source = "none"`
- [ ] provider plan 读取 Compose project 当前状态
- [ ] provider apply 对 `running` 执行 `docker compose up -d`
- [ ] provider apply 对 `stopped` 执行 `docker compose stop`
- [ ] provider apply 对 `absent` 执行 `docker compose down`
- [ ] `pull = never|missing|always` 映射为 Docker Compose v2 支持的命令行为
- [ ] `recreate = auto|always|never` 映射为 Docker Compose v2 支持的命令行为
- [ ] `remove_orphans = true` 时 up/down 命令附加 orphan 清理行为
- [ ] provider observed 摘要不记录 container 环境变量或 secret
- [ ] check 能检测 project 未运行、已停止或缺失

测试：

- [ ] NativeProvider fake runner 测试 `running` 生成 up 命令
- [ ] NativeProvider fake runner 测试 `stopped` 生成 stop 命令
- [ ] NativeProvider fake runner 测试 `absent` 生成 down 命令
- [ ] NativeProvider fake runner 测试 pull/recreate/remove_orphans flag
- [ ] provider plan 单测覆盖 running/no-op、stopped drift、absent/no-op
- [ ] Engine fake apply 后立即 plan 为 no-op
- [ ] check 返回码测试覆盖 Compose project stopped drift

示例：

- [ ] `examples/v2-docker-compose.dbf.hcl` 进入 fake runner apply 测试

文档：

- [ ] README 标明 Compose project 状态已可 apply/check
- [ ] 文档列出 state/pull/recreate/remove_orphans 的实际命令映射

验收：

```bash
dbf plan -f examples/v2-docker-compose.dbf.hcl --offline
make test
```

## Loop 7: Compose systemd unit 集成

目标：为每个 Compose project 生成 systemd unit，并由 systemd 管理开机启动和服务托管。

代码：

- [ ] 生成默认 unit 名称 `debianform-compose-<name>.service`
- [ ] 支持 `compose.service.enable`
- [ ] 支持 `compose.service.name`
- [ ] 支持 `after`
- [ ] 支持 `wanted_by`
- [ ] unit 内容包含 `Requires=docker.service`
- [ ] unit 内容包含默认 `After=docker.service network-online.target`
- [ ] unit `WorkingDirectory` 使用 compose directory
- [ ] unit `ExecStart` 使用 `docker compose -p <project> -f <file> up -d`
- [ ] unit `ExecStop` 使用 `docker compose -p <project> -f <file> stop`
- [ ] 编译 systemd unit 节点，复用现有 `systemd_unit` provider
- [ ] 编译 systemd daemon-reload operation
- [ ] 编译 compose service 节点，复用现有 service provider
- [ ] `state = "running"` 时默认 service enabled/running
- [ ] `state = "stopped"` 时默认 service enabled/stopped
- [ ] `state = "absent"` 时默认 service disabled/stopped，并由 project provider 执行 down
- [ ] project 状态 provider 和 systemd service 的执行顺序稳定且幂等

测试：

- [ ] ResourceGraph golden 覆盖 generated systemd unit
- [ ] plan golden 覆盖 unit 内容 diff
- [ ] 单测覆盖默认 unit 名称和自定义 unit 名称
- [ ] 单测覆盖 state running/stopped/absent 对 service desired 的影响
- [ ] Engine fake runner 测试 daemon-reload 和 service enable/start
- [ ] 负例覆盖非法 service name、非法 after/wanted_by 空值

示例：

- [ ] `examples/v2-docker-compose.dbf.hcl` 增加 systemd 默认行为注释

文档：

- [ ] README 增加 Compose systemd unit 简短说明
- [ ] Docker 需求文档旁补充当前 unit 模板限制

验收：

```bash
dbf plan -f examples/v2-docker-compose.dbf.hcl --offline
dbf plan -f examples/v2-docker-compose.dbf.hcl --offline --format json
make test
```

## Loop 8: MVP 端到端集成与 drift 用例

目标：在真实 Debian 测试机上验证 Docker Engine、daemon、Compose project、systemd 和 check/drift
形成 MVP 闭环。

代码/测试：

- [ ] 新增 libvirt case `docker-engine`
- [ ] 新增 libvirt case `docker-daemon`
- [ ] 新增 libvirt case `docker-compose`
- [ ] `docker-engine` 验证官方 repository 存在
- [ ] `docker-engine` 验证官方 packages 已安装
- [ ] `docker-engine` 验证 `docker.service` enabled/running
- [ ] `docker-daemon` 验证 `/etc/docker/daemon.json` 内容和权限
- [ ] `docker-compose` 使用最小 nginx 或 hello-world Compose project
- [ ] `docker-compose` 验证 `docker compose config` 已执行
- [ ] `docker-compose` 验证 Compose project running
- [ ] `docker-compose` 验证 generated systemd unit 内容
- [ ] `docker-compose` 验证 systemd service enabled/running
- [ ] drift case：手动修改 daemon JSON 后 `dbf check` 非零
- [ ] drift case：手动修改 compose.yaml 后 `dbf check` 非零
- [ ] drift case：手动 stop compose project 后 `dbf check` 非零
- [ ] drift case：手动 disable generated systemd unit 后 `dbf check` 非零

文档：

- [ ] README 标记 Docker / Compose MVP 可用范围
- [ ] 新增 Docker MVP 使用示例和限制
- [ ] 文档明确真实安装依赖 Docker 官方源可访问

验收：

```bash
make test
test/integration/libvirt/run-case.sh docker-engine
test/integration/libvirt/run-case.sh docker-daemon
test/integration/libvirt/run-case.sh docker-compose
```

MVP 完成判定：

- [ ] `docker { enable = true }` 在线 apply 后 check 为 no-op
- [ ] daemon settings 变更能 plan 出 file diff 和 restart
- [ ] Compose file 变更能 plan 出 file diff、validate 和 project converge
- [ ] Compose project stopped drift 能被 check 检出
- [ ] `make test` 和上述 libvirt cases 通过

## Loop 9: Docker users 和 docker group membership

目标：支持 `docker.users = ["deploy"]`，把用户加入 `docker` group，同时不接管用户完整定义。

范围：

- `docker.users`
- `group["docker"]`
- `user_group_membership`

代码：

- [ ] 新增 IR 或 graph-level membership spec
- [ ] 新增 provider kind `user_group_membership`
- [ ] 编译 `docker.users` 为 `docker` group 节点
- [ ] 编译每个用户的 docker group membership 节点
- [ ] membership 节点只管理补充组关系，不接管 user home/shell/uid
- [ ] membership 节点依赖 `docker` group
- [ ] 若同 host 也声明了同名 user，membership 节点依赖 user 节点
- [ ] plan/apply 输出提示重新登录后 group session 才会生效
- [ ] check 能检测用户缺失 docker group

测试：

- [ ] ResourceGraph golden 覆盖 docker group 和 membership
- [ ] NativeProvider fake runner 测试 `usermod -aG docker <user>` 行为
- [ ] provider plan 单测覆盖已在组内 no-op
- [ ] provider plan 单测覆盖用户不存在时诊断清晰
- [ ] 负例覆盖空用户名

示例：

- [ ] 新增 `examples/v2-docker-users.dbf.hcl`

文档：

- [ ] README 增加 docker users 示例
- [ ] 文档提示重新登录后权限生效

验收：

```bash
dbf plan -f examples/v2-docker-users.dbf.hcl --offline
make test
```

## Loop 10: package source 变体和冲突包处理

目标：补齐 `package.source = "debian" | "none" | "custom"` 和 `remove_conflicts`。

范围：

- `package.source = "debian"`
- `package.source = "none"`
- `package.source = "custom"`
- `package.remove_conflicts`
- Docker 冲突包检测和移除

代码：

- [ ] `source = "debian"` 不生成 Docker 官方 repository
- [ ] `source = "debian"` 默认安装 `docker.io` 和 `docker-compose-plugin`
- [ ] `source = "none"` 不生成 repository 和 package 节点
- [ ] `source = "none"` 仍允许 daemon、service、compose
- [ ] `source = "custom"` 不生成 repository/key/package，要求用户自己声明依赖或接受无 package 依赖
- [ ] 新增 conflict detection 节点或 provider plan 扩展
- [ ] 检测冲突包：
  - `docker.io`
  - `docker-doc`
  - `docker-compose`
  - `podman-docker`
  - `containerd`
  - `runc`
- [ ] `remove_conflicts = "auto"` plan 显示将替换的包，apply 移除需要替换的冲突包
- [ ] `remove_conflicts = true` 强制移除已安装冲突包
- [ ] `remove_conflicts = false` 检测到冲突包时 plan/apply 失败并提示用户
- [ ] 冲突包移除节点在官方 Docker packages 前执行

测试：

- [ ] HostSpec golden 覆盖 debian/none/custom source
- [ ] ResourceGraph golden 覆盖 `source = "debian"`
- [ ] ResourceGraph golden 覆盖 `source = "none"`
- [ ] provider fake runner 测试冲突包检测
- [ ] provider fake runner 测试冲突包移除
- [ ] 负例覆盖 `remove_conflicts = false` 时存在冲突包

示例：

- [ ] 新增 `examples/v2-docker-package-sources.dbf.hcl`

文档：

- [ ] 文档补充 source 变体语义和冲突包策略

验收：

```bash
dbf plan -f examples/v2-docker-package-sources.dbf.hcl --offline
make test
```

## Loop 11: Compose 多文件和运维细节增强

目标：补齐 Compose 管理中常见但不阻塞 MVP 的运维细节。

范围：

- 多个 compose file
- project name 更新策略
- orphan container 检测
- 更细粒度 drift 输出
- 更完整的 `pull` / `recreate` 行为

代码：

- [ ] 支持多个 `file` block 或明确拒绝并给出后续版本错误
- [ ] Compose provider observed 记录 project service/container 摘要
- [ ] check 输出 orphan container 诊断
- [ ] `remove_orphans = true` 时 apply 清理 orphan
- [ ] project name 变化时 plan 显示旧 project down、新 project up 的影响
- [ ] state 中保存 compose file/env file desired digest 和 project desired digest
- [ ] plan 中显示 Compose project state drift，而不是只显示底层命令

测试：

- [ ] provider fake runner 覆盖 orphan container
- [ ] provider fake runner 覆盖 project rename
- [ ] plan golden 覆盖 project state drift 输出
- [ ] check 返回码测试覆盖 orphan drift

文档：

- [ ] 文档补充 Compose drift 检测范围和限制

验收：

```bash
make test
```

## Loop 12: 未来扩展预留

目标：为后续版本留下清晰边界，不进入 MVP。

候选能力：

- [ ] HCL `spec` 自动生成 Compose YAML
- [ ] registry login
- [ ] rootless Docker
- [ ] Docker mirror 配置
- [ ] image lifecycle
- [ ] volume lifecycle
- [ ] network lifecycle
- [ ] secrets 管理
- [ ] Podman backend

进入实现前必须新增独立需求文档或扩展计划，不能直接塞进 MVP loop。
