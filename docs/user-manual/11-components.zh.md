# 11. 使用 component 安装二进制或源码构建工具

本章演示 DebianForm 的 `component`：把一组资源封装成可复用模板，再挂载到 host。
示例使用 source component，从远端本地 `file://` 源码构建一个小 C 程序并安装到
`/usr/local/bin/hello-from-source`。

本章示例已在 Debian 13 amd64 测试主机上验证通过。示例会通过 APT 安装 `gcc` 作为 build package；
源码文件由同一份 DebianForm 配置先写入远端，不依赖外部下载站点。

## 创建工作目录

```bash
mkdir -p debianform-manual/11-components
cd debianform-manual/11-components
```

## 写配置

创建 `site.dbf.hcl`：

```hcl
locals {
  hello_source = <<EOF
#include <stdio.h>

int main(void) {
  puts("hello from DebianForm component");
  return 0;
}
EOF
}

component "hello_from_source" {
  type    = "source"
  version = "1.0.0"

  source {
    url    = "file:///var/lib/debianform/manual/component-source/hello.c"
    sha256 = "c584e04a304b156188a80e373adb473ee26826652e39ca9b6a4b0321e3c85dc4"
  }

  build {
    packages = ["gcc"]

    commands = [
      ["cc", "-O2", "-Wall", "-o", "hello-from-source", "hello.c"],
    ]
    output      = "hello-from-source"
    source_name = "hello.c"
  }

  install {
    path  = "/usr/local/bin/hello-from-source"
    owner = "root"
    group = "root"
    mode  = "0755"
  }
}

host "manual1" {
  state {
    path      = "/var/lib/debianform/manual/11-state.json"
    lock_path = "/var/lock/debianform/manual/11-state.lock"
  }

  directories {
    directory "/var/lib/debianform/manual/component-source" {
      owner = "root"
      group = "root"
      mode  = "0755"
    }
  }

  files {
    file "/var/lib/debianform/manual/component-source/hello.c" {
      owner   = "root"
      group   = "root"
      mode    = "0644"
      content = local.hello_source
    }
  }

  components = [
    component.hello_from_source,
  ]
}
```

这份配置有两部分：

- host 先管理 `/var/lib/debianform/manual/component-source/hello.c`。
- component 再从 `file:///var/lib/debianform/manual/component-source/hello.c` 读取源码，校验 sha256，
  安装 build package，执行 `cc`，并把结果安装到 `/usr/local/bin/hello-from-source`。

`source.sha256` 必须是 64 位 sha256。这里的值对应 `hello_source` 的完整内容。

## 查看 component

运行：

```bash
dbf validate
dbf component inspect hello_from_source
```

当前 `component inspect` 主要展示 component input。本章的 component 没有 input，所以输出类似：

```json
{
  "name": "hello_from_source",
  "inputs": []
}
```

artifact download、build、install 的具体资源会在 plan 中展开。

## 离线 plan

运行：

```bash
dbf plan --offline
```

首次 plan 应该有 6 个 create：

```text
Summary: 6 create, 0 update, 0 delete, 0 no-op, 0 operations
```

关键资源地址包括：

- `host.manual1.components.hello_from_source.build.package["gcc"]`
- `host.manual1.components.hello_from_source.artifact.download["default"]`
- `host.manual1.components.hello_from_source.artifact.build[...]`
- `host.manual1.components.hello_from_source.artifact.install["/usr/local/bin/hello-from-source"]`

`artifact.build[...]` 的地址里会包含 build output cache 路径，路径随源码 sha 和 build 命令变化。

## 应用配置

运行：

```bash
dbf apply --auto-approve
dbf check
```

首次 apply 会安装 `gcc`，然后完成源码校验、构建和安装。`check` 应该回到 no changes：

```text
Summary: 0 create, 0 update, 0 delete, 6 no-op, 0 operations
```

## 验证安装结果

运行：

```bash
ssh manual1 '/usr/local/bin/hello-from-source; stat -c "%a %U %G %n" /usr/local/bin/hello-from-source /var/lib/debianform/manual/component-source/hello.c; sha256sum /var/lib/debianform/manual/component-source/hello.c'
```

预期类似：

```text
hello from DebianForm component
755 root root /usr/local/bin/hello-from-source
644 root root /var/lib/debianform/manual/component-source/hello.c
c584e04a304b156188a80e373adb473ee26826652e39ca9b6a4b0321e3c85dc4  /var/lib/debianform/manual/component-source/hello.c
```

确认 state 里记录了 download、build package、build 和 install 资源：

```bash
ssh manual1 'python3 - <<'"'"'PY'"'"'
import json

with open("/var/lib/debianform/manual/11-state.json", encoding="utf-8") as f:
    resources = json.load(f).get("resources", {})

for key in [
    "host.manual1.components.hello_from_source.artifact.download[\"default\"]",
    "host.manual1.components.hello_from_source.artifact.install[\"/usr/local/bin/hello-from-source\"]",
    "host.manual1.components.hello_from_source.build.package[\"gcc\"]",
]:
    assert key in resources, key

assert any(k.startswith("host.manual1.components.hello_from_source.artifact.build[") for k in resources), resources
print("state resources ok")
PY'
```

## 制造安装目标漂移

删除安装后的二进制：

```bash
ssh manual1 'rm -f /usr/local/bin/hello-from-source'
```

运行：

```bash
dbf check
```

预期失败，只显示 install 资源需要重新创建：

```text
+ host.manual1.components.hello_from_source.artifact.install["/usr/local/bin/hello-from-source"]
  install component binary /usr/local/bin/hello-from-source

Summary: 1 create, 0 update, 0 delete, 5 no-op, 0 operations
dbf: remote state does not match configuration
```

## 修复漂移

运行：

```bash
dbf apply --auto-approve
dbf check
ssh manual1 '/usr/local/bin/hello-from-source; stat -c "%a %U %G" /usr/local/bin/hello-from-source'
```

DebianForm 会从 component build cache 重新安装目标文件，`check` 回到 no changes。

## 什么时候用 component

适合封装成 component 的内容：

- 一组总是一起出现的文件、用户、服务和 systemd unit。
- 一个需要固定版本和 sha256 的外部二进制。
- 一个需要源码下载、构建、安装的工具。
- 一个希望通过 input 参数复用到多个 host 的服务模板。

不适合立刻抽 component 的内容：

- 只有一个 host 用到的一两行简单配置。
- 还在频繁试错、边界没有稳定的配置。

## 本章完整命令

```bash
mkdir -p debianform-manual/11-components
cd debianform-manual/11-components

cat > site.dbf.hcl <<'EOF'
locals {
  hello_source = <<SRC
#include <stdio.h>

int main(void) {
  puts("hello from DebianForm component");
  return 0;
}
SRC
}

component "hello_from_source" {
  type    = "source"
  version = "1.0.0"

  source {
    url    = "file:///var/lib/debianform/manual/component-source/hello.c"
    sha256 = "c584e04a304b156188a80e373adb473ee26826652e39ca9b6a4b0321e3c85dc4"
  }

  build {
    packages = ["gcc"]

    commands = [
      ["cc", "-O2", "-Wall", "-o", "hello-from-source", "hello.c"],
    ]
    output      = "hello-from-source"
    source_name = "hello.c"
  }

  install {
    path  = "/usr/local/bin/hello-from-source"
    owner = "root"
    group = "root"
    mode  = "0755"
  }
}

host "manual1" {
  state {
    path      = "/var/lib/debianform/manual/11-state.json"
    lock_path = "/var/lock/debianform/manual/11-state.lock"
  }

  directories {
    directory "/var/lib/debianform/manual/component-source" {
      owner = "root"
      group = "root"
      mode  = "0755"
    }
  }

  files {
    file "/var/lib/debianform/manual/component-source/hello.c" {
      owner   = "root"
      group   = "root"
      mode    = "0644"
      content = local.hello_source
    }
  }

  components = [
    component.hello_from_source,
  ]
}
EOF

dbf validate
dbf component inspect hello_from_source
dbf plan --offline
dbf apply --auto-approve
dbf check
ssh manual1 '/usr/local/bin/hello-from-source; stat -c "%a %U %G %n" /usr/local/bin/hello-from-source /var/lib/debianform/manual/component-source/hello.c; sha256sum /var/lib/debianform/manual/component-source/hello.c'
ssh manual1 'python3 - <<'"'"'PY'"'"'
import json

with open("/var/lib/debianform/manual/11-state.json", encoding="utf-8") as f:
    resources = json.load(f).get("resources", {})

for key in [
    "host.manual1.components.hello_from_source.artifact.download[\"default\"]",
    "host.manual1.components.hello_from_source.artifact.install[\"/usr/local/bin/hello-from-source\"]",
    "host.manual1.components.hello_from_source.build.package[\"gcc\"]",
]:
    assert key in resources, key

assert any(k.startswith("host.manual1.components.hello_from_source.artifact.build[") for k in resources), resources
print("state resources ok")
PY'

ssh manual1 'rm -f /usr/local/bin/hello-from-source'
dbf check || true
dbf apply --auto-approve
dbf check
ssh manual1 '/usr/local/bin/hello-from-source; stat -c "%a %U %G" /usr/local/bin/hello-from-source'
```

## 清理

如果要删除本章创建的源码、build cache、安装结果和 state：

```bash
ssh manual1 'rm -f /usr/local/bin/hello-from-source; rm -rf /var/lib/debianform/manual/component-source /var/cache/debianform/components/hello_from_source /var/lib/debianform/manual/11-state.json /var/lock/debianform/manual/11-state.lock /var/lock/debianform/manual/11-state.lock.d'
```

如果本章安装的 `gcc` 不是其他工作需要的，可以额外卸载：

```bash
ssh manual1 'DEBIAN_FRONTEND=noninteractive apt-get remove -y gcc'
```
