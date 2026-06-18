# debianform

`debianform` is a small Debian configuration manager. The CLI command is `dbf`.

This MVP uses a restricted `.dbf.hcl` syntax, runs locally, connects to remote Debian hosts through `ssh` as `root`, and stores state on an SSH host with a remote `flock` lock.

## Install

From this repository:

```bash
go build -o dbf ./cmd/dbf
install -m 0755 dbf ~/.local/bin/dbf
```

Or install directly into `GOBIN` / `GOPATH/bin`:

```bash
go install ./cmd/dbf
```

Make sure the install directory is in `PATH`.

Requirements:

- Local machine: Go and `ssh`.
- Remote Debian hosts: SSH login as `root`.
- State host: `flock`, `base64`, `sh`, and write access to the configured state path.

## Configuration Loading

By default, `dbf` reads all `*.dbf.hcl` files in the current directory, sorted by filename. This is similar to Terraform's root module behavior, but it is not recursive.

```bash
dbf validate
dbf plan
dbf apply
```

Use `-f` when you want to load exactly one file:

```bash
dbf validate -f examples/main.dbf.hcl
```

## Workflow

`dbf plan` does this:

1. Load all selected `.dbf.hcl` files.
2. Evaluate `locals`, `for_each`, `each.key`, `each.value`, and `file()`.
3. Build stable resource addresses such as `debian_file.motd` or `debian_file.host_file["ksvm201"]`.
4. Read the SSH state file.
5. Read actual remote host state over SSH.
6. Compare desired config with the remote host state and print a plan.

`dbf apply` first acquires the remote state lock, then re-reads state and recalculates the plan before making changes. This avoids applying an old plan after another person has changed the shared state.

## Commands

```bash
dbf validate
dbf plan
dbf apply
dbf check

dbf plan --host server1
dbf apply --auto-approve
```

## Handlers

Use `notify` and `handler` when a resource change should trigger a command after all normal resources are applied.

```hcl
handler "reload_nginx" {
  host    = "server1"
  command = "systemctl reload nginx"
}

debian_file "nginx_conf" {
  host   = "server1"
  path   = "/etc/nginx/nginx.conf"
  source = "${path.module}/files/nginx.conf"

  notify = [
    handler.reload_nginx,
  ]
}
```

A handler runs only if a notifying resource actually changed. If multiple resources notify the same handler, it runs once, after all normal resources complete successfully.

## MVP Resources

- `debian_package`
- `debian_file`
- `debian_directory`
- `debian_service`
- `debian_networkd_file`

All resources use `host = "server1"`, where `server1` can be an SSH config `Host` alias. `dbf` always connects as `root`.

## Supported HCL Subset

- Blocks: `state "ssh"`, optional `host "name"`, and resource blocks.
- Handlers: `handler "name"` blocks and resource `notify`.
- Strings, booleans, numbers, lists, maps, heredocs.
- `file("path")`.
- `toset(["a", "b"])` for Terraform-style string sets.
- `${path.module}`, `${each.key}`, `${each.value}` interpolation.
- `locals { ... }` and `local.name` references.
- `for_each` over maps or lists of strings.

For the current example, see [examples/main.dbf.hcl](examples/main.dbf.hcl).
