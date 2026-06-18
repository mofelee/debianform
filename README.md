# debianform

`debianform` is a small Debian configuration manager. The CLI command is `dbf`.

This MVP uses a restricted `.dbf.hcl` syntax, runs locally, connects to remote Debian hosts through `ssh` as `root`, and stores state on an SSH host with a remote `flock` lock.

## Build

```bash
go build -o dbf ./cmd/dbf
```

## Commands

```bash
dbf validate -f examples/main.dbf.hcl
dbf plan -f examples/main.dbf.hcl
dbf apply -f examples/main.dbf.hcl --auto-approve
dbf check -f examples/main.dbf.hcl
```

## MVP Resources

- `debian_package`
- `debian_file`
- `debian_directory`
- `debian_service`
- `debian_networkd_file`

All resources use `host = "server1"`, where `server1` can be an SSH config `Host` alias. `dbf` always connects as `root`.

## Supported HCL Subset

- Blocks: `state "ssh"`, optional `host "name"`, and resource blocks.
- Strings, booleans, numbers, lists, maps, heredocs.
- `file("path")`.
- `toset(["a", "b"])` for Terraform-style string sets.
- `${path.module}`, `${each.key}`, `${each.value}` interpolation.
- `locals { ... }` and `local.name` references.
- `for_each` over maps or lists of strings.

For the current example, see [examples/main.dbf.hcl](examples/main.dbf.hcl).
