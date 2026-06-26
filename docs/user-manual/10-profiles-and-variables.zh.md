# 10. 使用 profile、variable 和多环境参数

本章演示两个复用机制：

- `profile` 用来沉淀可复用的配置片段，再由 host `imports` 引入。
- `variable` 用来把同一份配置按不同环境参数渲染。

本章示例已在 Debian 13 amd64 测试主机上验证通过。示例只写
`/etc/debianform-manual/profile-demo` 下的文件，不安装软件包，也不修改全局系统设置。

## 创建工作目录

```bash
mkdir -p debianform-manual/10-profiles-and-variables
cd debianform-manual/10-profiles-and-variables
```

## 写配置

创建 `site.dbf.hcl`：

```hcl
variable "environment" {
  type    = string
  default = "dev"

  validation {
    condition     = contains(["dev", "staging", "prod"], var.environment)
    error_message = "environment must be dev, staging, or prod."
  }
}

variable "owner" {
  type    = string
  default = "platform"
}

variable "feature_flags" {
  type    = list(string)
  default = []
}

variable "settings" {
  type = object({
    color  = string
    reload = optional(bool, false)
  })

  default = {
    color = "blue"
  }
}

profile "manual_base" {
  directories {
    directory "/etc/debianform-manual/profile-demo" {
      owner = "root"
      group = "root"
      mode  = "0755"
    }
  }

  files {
    file "/etc/debianform-manual/profile-demo/common.json" {
      owner = "root"
      group = "root"
      mode  = "0644"

      content = jsonencode({
        environment = var.environment
        owner       = var.owner
        flags       = var.feature_flags
        settings    = var.settings
      })
    }
  }
}

profile "manual_service" {
  imports = [profile.manual_base]

  files {
    file "/etc/debianform-manual/profile-demo/service.txt" {
      owner   = "root"
      group   = "root"
      mode    = "0644"
      content = "managed by service profile\n"
    }
  }
}

host "manual1" {
  imports = [profile.manual_service]

  state {
    path      = "/var/lib/debianform/manual/10-state.json"
    lock_path = "/var/lock/debianform/manual/10-state.lock"
  }

  files {
    file "/etc/debianform-manual/profile-demo/host.json" {
      owner = "root"
      group = "root"
      mode  = "0644"

      content = jsonencode({
        host        = "manual1"
        environment = var.environment
        owner       = var.owner
        reload      = var.settings.reload
      })
    }
  }
}
```

再创建两个变量文件。

`dev.dbfvars`：

```hcl
environment   = "dev"
owner         = "dev-team"
feature_flags = ["debug", "local-cache"]

settings = {
  color  = "green"
  reload = false
}
```

`prod.dbfvars`：

```hcl
environment   = "prod"
owner         = "platform-team"
feature_flags = ["audit", "alerts"]

settings = {
  color  = "red"
  reload = true
}
```

## profile 合并规则

`profile.manual_service` 通过 `imports = [profile.manual_base]` 引入基础 profile。
`host.manual1` 再通过 `imports = [profile.manual_service]` 引入服务 profile。

合并时要记住：

- 被 import 的 profile 先合入，当前 profile 或 host 后合入。
- map 会递归合并。
- list 会去重追加。
- 标量值由后合入的一侧覆盖。
- profile 不能声明 host-only 字段，例如 `system.hostname`、`system.architecture`、`system.codename`。

本章的 `common.json` 来自 `manual_base`，`service.txt` 来自 `manual_service`，`host.json` 来自 host 自己。

## 查看变量有效值

运行：

```bash
dbf variable inspect -var-file prod.dbfvars
```

输出会列出每个变量的有效值。节选如下：

```json
{
  "name": "environment",
  "type": "string",
  "default": "prod"
}
```

当前命令的输出字段名是 `default`，但这里展示的是变量合并后的有效值。

## 离线预览两个环境

先看 dev：

```bash
dbf plan --offline -var-file dev.dbfvars
```

plan 中的文件内容应包含：

```json
{"environment":"dev","flags":["debug","local-cache"],"owner":"dev-team","settings":{"color":"green","reload":false}}
```

再看 prod：

```bash
dbf plan --offline -var-file prod.dbfvars
```

plan 中的文件内容应包含：

```json
{"environment":"prod","flags":["audit","alerts"],"owner":"platform-team","settings":{"color":"red","reload":true}}
```

## 应用 prod 参数

运行：

```bash
dbf validate -var-file prod.dbfvars
dbf apply --auto-approve -var-file prod.dbfvars
dbf check -var-file prod.dbfvars
```

首次 apply 应该创建 4 个资源：

```text
Summary: 4 create, 0 update, 0 delete, 0 no-op, 0 operations
```

## 验证远端文件

运行：

```bash
ssh manual1 'cat /etc/debianform-manual/profile-demo/common.json; printf "\n"; cat /etc/debianform-manual/profile-demo/service.txt; cat /etc/debianform-manual/profile-demo/host.json; printf "\n"; stat -c "%a %U %G %n" /etc/debianform-manual/profile-demo /etc/debianform-manual/profile-demo/common.json /etc/debianform-manual/profile-demo/service.txt /etc/debianform-manual/profile-demo/host.json'
```

预期内容包含：

```text
{"environment":"prod","flags":["audit","alerts"],"owner":"platform-team","settings":{"color":"red","reload":true}}
managed by service profile
{"environment":"prod","host":"manual1","owner":"platform-team","reload":true}
```

权限类似：

```text
755 root root /etc/debianform-manual/profile-demo
644 root root /etc/debianform-manual/profile-demo/common.json
644 root root /etc/debianform-manual/profile-demo/service.txt
644 root root /etc/debianform-manual/profile-demo/host.json
```

## 用 -var 临时覆盖

`-var` 的优先级高于 `-var-file`。运行：

```bash
dbf plan --offline -var-file prod.dbfvars -var owner=release-team
```

plan 中应显示 `owner` 被改成 `release-team`：

```json
{"environment":"prod","host":"manual1","owner":"release-team","reload":true}
```

如果这不是一次真实变更，只运行离线 plan 即可，不要 apply。

## 验证变量校验失败

运行：

```bash
dbf validate -var environment=qa
```

预期失败：

```text
dbf: site.dbf.hcl:5:variable["environment"].validation[0]: validation failed for variable "environment": environment must be dev, staging, or prod.
```

## 变量来源优先级

常见变量来源从低到高是：

- 环境变量 `DBF_VAR_name=value`。
- `debianform.dbfvars` 和 `debianform.dbfvars.json`。
- `*.auto.dbfvars` 和 `*.auto.dbfvars.json`。
- 显式 `-var-file path`。
- 命令行 `-var name=value`。

多目录配置时，自动变量文件按参与配置目录首次出现顺序加载；后面的目录可以覆盖前面目录的同名变量。
本手册建议教程和 CI 中优先使用显式 `-var-file`，这样命令的输入更容易审阅。

## 本章完整命令

```bash
mkdir -p debianform-manual/10-profiles-and-variables
cd debianform-manual/10-profiles-and-variables

cat > site.dbf.hcl <<'EOF'
variable "environment" {
  type    = string
  default = "dev"

  validation {
    condition     = contains(["dev", "staging", "prod"], var.environment)
    error_message = "environment must be dev, staging, or prod."
  }
}

variable "owner" {
  type    = string
  default = "platform"
}

variable "feature_flags" {
  type    = list(string)
  default = []
}

variable "settings" {
  type = object({
    color  = string
    reload = optional(bool, false)
  })

  default = {
    color = "blue"
  }
}

profile "manual_base" {
  directories {
    directory "/etc/debianform-manual/profile-demo" {
      owner = "root"
      group = "root"
      mode  = "0755"
    }
  }

  files {
    file "/etc/debianform-manual/profile-demo/common.json" {
      owner = "root"
      group = "root"
      mode  = "0644"

      content = jsonencode({
        environment = var.environment
        owner       = var.owner
        flags       = var.feature_flags
        settings    = var.settings
      })
    }
  }
}

profile "manual_service" {
  imports = [profile.manual_base]

  files {
    file "/etc/debianform-manual/profile-demo/service.txt" {
      owner   = "root"
      group   = "root"
      mode    = "0644"
      content = "managed by service profile\n"
    }
  }
}

host "manual1" {
  imports = [profile.manual_service]

  state {
    path      = "/var/lib/debianform/manual/10-state.json"
    lock_path = "/var/lock/debianform/manual/10-state.lock"
  }

  files {
    file "/etc/debianform-manual/profile-demo/host.json" {
      owner = "root"
      group = "root"
      mode  = "0644"

      content = jsonencode({
        host        = "manual1"
        environment = var.environment
        owner       = var.owner
        reload      = var.settings.reload
      })
    }
  }
}
EOF

cat > dev.dbfvars <<'EOF'
environment   = "dev"
owner         = "dev-team"
feature_flags = ["debug", "local-cache"]

settings = {
  color  = "green"
  reload = false
}
EOF

cat > prod.dbfvars <<'EOF'
environment   = "prod"
owner         = "platform-team"
feature_flags = ["audit", "alerts"]

settings = {
  color  = "red"
  reload = true
}
EOF

dbf variable inspect -var-file prod.dbfvars
dbf plan --offline -var-file dev.dbfvars
dbf plan --offline -var-file prod.dbfvars
dbf validate -var-file prod.dbfvars
dbf apply --auto-approve -var-file prod.dbfvars
dbf check -var-file prod.dbfvars
ssh manual1 'cat /etc/debianform-manual/profile-demo/common.json; printf "\n"; cat /etc/debianform-manual/profile-demo/service.txt; cat /etc/debianform-manual/profile-demo/host.json; printf "\n"; stat -c "%a %U %G %n" /etc/debianform-manual/profile-demo /etc/debianform-manual/profile-demo/common.json /etc/debianform-manual/profile-demo/service.txt /etc/debianform-manual/profile-demo/host.json'
dbf plan --offline -var-file prod.dbfvars -var owner=release-team
dbf validate -var environment=qa || true
```

## 清理

如果要清理本章创建的远端文件和 state：

```bash
ssh manual1 'rm -rf /etc/debianform-manual/profile-demo /var/lib/debianform/manual/10-state.json /var/lock/debianform/manual/10-state.lock /var/lock/debianform/manual/10-state.lock.d'
```
