# README asciinema 演示录制指南

本文档记录 GitHub README 终端演示的生成方式。目标是在 README 中直接展示
`docs/demo/debianform-quickstart.svg`，同时保留可复现的 asciinema 原始录制
`docs/demo/debianform-quickstart.cast`。

## 依赖

- `asciinema`：录制 `.cast`。
- `node` + `npx`：通过 `svg-term-cli` 渲染 animated SVG。
- `go`：构建当前仓库的 `dbf`。
- `virsh`、`ssh`，以及 `/root/.codex/skills/virsh-test-host/scripts/virsh-test-host.sh`。
- 可用的 libvirt URI。当前环境通常是 `LIBVIRT_DEFAULT_URI=qemu+ssh://ks/system`。

录制脚本会创建并销毁一个固定名称的临时 VM：`dbf-test-readme-demo`。销毁逻辑只作用于
这个 `dbf-test` 前缀的域名。

## 生成步骤

在仓库根目录运行：

```bash
docs/demo/record-readme-demo.sh
docs/demo/render-readme-demo.sh
```

第一条命令会：

- 构建临时 `dbf` 二进制，使用 `go build -buildvcs=false`，避免 README 演示里出现录制时工作区的
  `+dirty` 状态。
- 使用 `virsh-test-host` 创建 Debian 13 临时主机。
- 在录制外生成临时 SSH config，录屏中只展示稳定别名 `dbf-demo-host`。
  临时 config 顶部会 `Include ~/.ssh/config`，让 `ProxyJump ks` 这类本机跳板别名仍可解析。
- 录制本地 `dbf version`、远端 Debian 版本/架构、`validate`、在线 `plan`、真实 `apply`、
  第二次 no-op `plan` 和 `check`。
- 退出时自动销毁 `dbf-test-readme-demo`。

第二条命令会把 `.cast` 渲染成 README 使用的 SVG。

## 可调参数

常用环境变量：

```bash
DBF_DEMO_DOMAIN=dbf-test-readme-demo
DBF_DEMO_HOST_ALIAS=dbf-demo-host
DBF_DEMO_COLS=90
DBF_DEMO_ROWS=28
DBF_DEMO_IDLE_TIME_LIMIT=2
DBF_TEST_POOL=vm
DBF_TEST_NETWORK=default
```

如果临时主机没有被自动清理，可以手动执行：

```bash
DBF_TEST_NAME=dbf-test-readme-demo \
  /root/.codex/skills/virsh-test-host/scripts/virsh-test-host.sh destroy dbf-test-readme-demo
```

## 发布前检查

```bash
asciinema cat docs/demo/debianform-quickstart.cast >/dev/null
test -s docs/demo/debianform-quickstart.svg
rg '192\\.168\\.|ProxyJump|IdentityFile|/root/\\.ssh' docs/demo/debianform-quickstart.cast docs/demo/debianform-quickstart.svg
```

最后一条命令应无输出。若出现本地 IP、跳板机或私钥路径，先调整脚本再重录。
