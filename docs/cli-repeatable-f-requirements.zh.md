# CLI 可重复 `-f` 需求文档

## 背景

当前 `dbf` 的多数配置命令支持 `-f file`，用于精确指定一个 `.dbf.hcl` 配置文件。
不传 `-f` 时，CLI 会读取当前目录下所有 `*.dbf.hcl` 文件，并按文件名排序后合并解析。

现有实现中，`-f` 是单值字符串 flag。用户如果多次传入 `-f`：

```bash
dbf validate -f base.dbf.hcl -f app.dbf.hcl
```

实际只有最后一个文件会生效，等价于：

```bash
dbf validate -f app.dbf.hcl
```

这与用户对“重复指定多个输入文件”的直觉不一致，也缺少一种常见能力：只加载一组明确指定的
文件，而不是加载当前目录下的全部 `*.dbf.hcl`。

## 目标

支持重复传入 `-f`，让 CLI 可以显式加载多个配置文件：

```bash
dbf validate -f base.dbf.hcl -f app.dbf.hcl
dbf plan -f base.dbf.hcl -f host.dbf.hcl --offline
dbf fmt -f base.dbf.hcl -f app.dbf.hcl
dbf component inspect -f components.dbf.hcl -f hosts.dbf.hcl reverse_proxy
```

多个 `-f` 指定的文件应全部传入现有 `ParseFiles([]string)` 流程，按命令行出现顺序解析。

## 适用命令

可重复 `-f` 应覆盖当前已经支持 `-f` 的命令：

- `dbf validate`
- `dbf plan`
- `dbf apply`
- `dbf check`
- `dbf fmt`
- `dbf component inspect`

## 行为规则

### 不传 `-f`

保持现有行为不变：

- 读取当前目录下所有 `*.dbf.hcl`
- 按文件名排序
- 没有匹配文件时报错

示例：

```bash
dbf validate
```

### 传入一个 `-f`

保持现有行为不变：

- 只读取指定的一个文件
- 不自动读取该文件同目录下的其他 `.dbf.hcl`
- 不把参数当目录处理

示例：

```bash
dbf validate -f app.dbf.hcl
```

### 传入多个 `-f`

新增行为：

- 读取所有通过 `-f` 显式指定的文件
- 文件顺序保持命令行顺序
- 不再读取当前目录下未显式指定的其他 `*.dbf.hcl`
- 每个 `-f` 仍然只接受一个文件路径

示例：

```bash
dbf validate -f base.dbf.hcl -f app.dbf.hcl
```

应解析：

```text
base.dbf.hcl
app.dbf.hcl
```

而不是只解析 `app.dbf.hcl`。

### `fmt` 行为

`dbf fmt` 在传入多个 `-f` 时，应只格式化显式指定的文件，并继续输出当前格式：

```text
formatted N file(s)
```

其中 `N` 是内容实际发生变化的文件数量，不是传入文件总数。

### plan command metadata

`plan` 输出中的 command file 信息应继续使用现有 `commandFile(files)` 逻辑。

- 单文件：显示该文件路径
- 多文件：显示以逗号连接的文件路径

本需求不要求更改 plan JSON schema。

## 非目标

本次不实现以下能力：

- 不支持 `-f a.dbf.hcl,b.dbf.hcl` 这种逗号分隔语法
- 不支持 `-f dir/` 递归读取目录
- 不支持 glob 参数由程序内部展开，例如 `-f "configs/*.dbf.hcl"`
- 不支持 `--file` 长选项，除非另有独立需求
- 不改变 `.dbf.hcl` 文件之间的 merge、duplicate block、source ref 语义

## 兼容性

这是一个小的行为变更：

- 旧行为：重复 `-f` 时最后一个覆盖前一个
- 新行为：重复 `-f` 时全部文件都会被解析

该变化更符合 CLI 用户直觉，但可能影响依赖旧覆盖行为的脚本。预计这种脚本很少见。

为了降低误用风险，文档和 help 文案应明确 `-f` 可以重复。

## 实现建议

可以新增一个自定义 flag 类型保存文件列表，例如：

```go
type fileFlags []string
```

实现 `flag.Value`：

- `String() string`
- `Set(value string) error`

然后把当前的：

```go
file := fs.String("f", "", "configuration file")
```

替换为可重复 flag，并让 `configFiles` 接收 `[]string`：

```go
var filesFlag fileFlags
fs.Var(&filesFlag, "f", "configuration file; may be repeated")
```

`configFiles` 的建议语义：

```text
如果 filesFlag 非空，返回它的副本
如果 filesFlag 为空，按当前逻辑 glob 当前目录 *.dbf.hcl
```

注意保持调用方不能意外修改内部 slice。

## 测试要求

至少补充以下测试：

- `configFiles(nil)` 仍然按当前目录 `*.dbf.hcl` 文件名排序
- `configFiles([]string{"custom.dbf.hcl"})` 返回单个文件
- `configFiles([]string{"base.dbf.hcl", "app.dbf.hcl"})` 按传入顺序返回两个文件
- `run validate -f base -f app` 会把两个文件一起解析
- `run fmt -f a -f b` 只格式化显式传入的两个文件
- `component inspect -f components -f hosts name` 使用两个文件解析 component

如果新增自定义 flag 类型，应补充独立单测覆盖重复 `Set` 后的顺序。

## 文档要求

实现时需要同步更新：

- `docs/cli.zh.md`
- `README.md` 中涉及 `-f` 的说明，如有必要
- CLI `usage()` 输出

文案应避免继续写“指定单个配置文件”，建议改为：

```text
`-f file` 可重复。传入一个或多个 `-f` 时，只读取显式指定的文件；
不传时读取当前目录所有 `*.dbf.hcl`。
```

## 验收标准

以下命令行为应符合预期：

```bash
dbf validate -f base.dbf.hcl -f app.dbf.hcl
```

同时解析 `base.dbf.hcl` 和 `app.dbf.hcl`。

```bash
dbf plan -f base.dbf.hcl -f host.dbf.hcl --offline
```

生成 plan 时使用这两个文件，不读取当前目录其他 `.dbf.hcl`。

```bash
dbf fmt -f base.dbf.hcl -f app.dbf.hcl
```

只检查并格式化这两个文件。

最终验收：

```bash
make test
```

必须通过。
