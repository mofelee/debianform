# 真实部署模板：systemd app

本文档给出一个可本地验证的小型部署案例：
`examples/v2-realistic-systemd-app.dbf.hcl`。它不依赖外部下载、不包含真实 secret，
适合作为第一台低风险 Debian 13 主机上的模板起点。

## 覆盖范围

这个示例声明了一个完整但很小的 systemd 应用：

- `groups.group "demoapp"`：创建系统 group。
- `users.user "demoapp"`：创建系统用户，使用 `/usr/sbin/nologin`。
- `directories.directory`：管理 `/etc/demoapp` 和 `/var/lib/demoapp` 权限。
- `files.file`：写入配置文件和可执行脚本。
- `systemd.service_unit "demoapp"`：生成 `/etc/systemd/system/demoapp.service`。
- `services.service "demoapp"`：声明服务 enabled 且 running。

管理连接仍然使用 DebianForm 当前支持的 root SSH 模型；服务进程本身通过
`systemd.service_unit.user/group` 以 `demoapp` 低权限运行。

## 本地验收

只做本地语法和离线 plan 预览：

```bash
dbf validate -f examples/v2-realistic-systemd-app.dbf.hcl
dbf plan -f examples/v2-realistic-systemd-app.dbf.hcl --offline
```

预期 plan 会包含：

- 创建 `demoapp` group 和 user。
- 创建 `/etc/demoapp`、`/var/lib/demoapp`。
- 写入 `/etc/demoapp/config.env` 和 `/usr/local/bin/demoapp-worker`。
- 写入 `demoapp.service` 并触发 `systemctl daemon-reload`。
- enable/start `demoapp.service`。

## 真实主机试用

在低风险 Debian 13 测试主机上试用时，先把 `host "demoapp1"` 中的 SSH 和 state
配置按 quickstart 补齐，然后按顺序执行：

```bash
dbf validate -f site.dbf.hcl
dbf plan -f site.dbf.hcl
dbf apply -f site.dbf.hcl
dbf plan -f site.dbf.hcl
dbf check -f site.dbf.hcl
```

第一次 apply 后，再次 online plan 应该接近 no-op；`check` 应返回 0。若有人手动修改
unit、文件或服务状态，`check` 应输出 drift 并返回非零。

## 定制建议

- 把 `demoapp` 改成真实服务名，并保持 user/group、目录和 unit 名称一致。
- 把非敏感配置放在普通 `files.file` 中。
- 真实 token、私钥或密码使用 `variable + files.file sensitive = true` 模式，不要直接写入示例配置。
- 如果服务需要外部二进制，优先使用 `component` 的 artifact 能力，并固定 sha256。
- 对生产数据目录和高风险服务，评估是否需要 `lifecycle.prevent_destroy`。

相关文档：

- [quickstart](quickstart.zh.md)
- [CLI 文档](cli.zh.md)
- [operations runbook](operations-runbook.zh.md)
- [support matrix](support-matrix.zh.md)
- [v2 systemd service units](v2-systemd-service-units.md)
