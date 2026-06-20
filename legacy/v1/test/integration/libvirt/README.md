# Libvirt integration tests

每个 `cases/<name>/` 目录都是一个独立集成测试。运行器为每个目录创建新的 Debian
13 qcow2 overlay、cloud-init seed、libvirt network 和 VM；场景之间不复用磁盘或
系统状态。

## 场景协议

一个场景至少包含两个连续编号的配置：

```text
cases/example/
├── setup.sh           # 可选，只执行一次
├── 1.dbf.hcl
├── 1.plan
├── 1.check.sh
├── 2.dbf.hcl
├── 2.drift.sh       # 可选
├── 2.plan
├── 2.check.sh
├── 3.dbf.hcl        # 最终销毁配置
├── 3.plan
└── 3.check.sh
```

规则：

1. 配置从 `1.dbf.hcl` 开始连续编号并按顺序执行。
   可选 `setup.sh` 在 VM 就绪后、第一步之前执行一次。
2. 每一步依次执行 `validate`、`plan`、`apply`、`check` 和第二次 no-op `plan`。
3. `<N>.plan` 每行写一个资源或 handler 地址。实际计划地址必须与它完全一致，
   多一个或少一个都会失败。
4. `<N>.check.sh` 使用 `assert_remote "说明" "命令"` 逐项检查远端真实状态。
   显式检查数量不能少于该步 `.plan` 中的地址数量。
5. 可选 `<N>.drift.sh` 使用 `run_remote "说明" "命令"` 制造漂移。运行器会先要求
   `dbf check` 失败，再执行该步的 plan/apply。
6. 最后一个 `.dbf.hcl` 必须只保留 state 配置并包含零个资源。
7. 最后一步的 `.plan` 必须列出所有待销毁资源；应用后 state 的 `resources` 必须
   为空，最终 `.check.sh` 必须逐项确认主机上的对象已经消失或恢复到空白 VM 状态。

所有配置都声明 `host "debian_ci"`，其中地址使用 `__DBF_VM_IP__`。运行器启动 VM
后会在工作副本中替换该地址，并写入 `${path.module}/id_ed25519`。源场景文件保持
可静态验证，不会被修改。每个场景工作副本还会得到一个 `app_key.pub`，供
authorized key 测试通过 `source = "${path.module}/app_key.pub"` 读取。

## 运行

运行全部场景，每个场景使用一台新 VM：

```bash
make test-legacy-v1-integration
```

只运行一个目录：

```bash
make test-legacy-v1-integration-case CASE=files
```

只检查目录协议、HCL 和 shell 语法，不启动 VM：

```bash
make test-legacy-v1-integration-layout
```

可用环境变量：

| 变量 | 说明 |
| --- | --- |
| `DBF_INTEGRATION_CASE` | 只运行指定场景目录 |
| `DBF_INTEGRATION_WORKDIR` | 指定工作目录 |
| `DBF_INTEGRATION_KEEP_WORKDIR=1` | 测试后保留工作目录 |
| `DBF_INTEGRATION_ARTIFACT_DIR` | 指定失败诊断目录 |
| `DBF_INTEGRATION_IMAGE_CACHE` | 指定 Debian 基础镜像缓存 |
| `DBF_INTEGRATION_DISABLE_KVM=1` | 强制使用 QEMU 软件模拟 |
