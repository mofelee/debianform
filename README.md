# debianform

`debianform` is a small Debian configuration manager. The CLI command is `dbf`.

This MVP uses a restricted `.dbf.hcl` syntax, runs locally, connects to remote Debian hosts through `ssh` as `root`, and stores state on an SSH host with a remote `flock` lock.

## Install

From this repository:

```bash
make build
install -m 0755 dbf ~/.local/bin/dbf
```

For a released version, install a specific Git tag directly into `GOBIN` / `GOPATH/bin`:

```bash
go install github.com/mofelee/debianform/cmd/dbf@v0.1.0
```

Make sure the install directory is in `PATH`.

Check the installed version:

```bash
dbf --version
dbf version
```

## Versioning

Git tags are the source of truth for the DebianForm version. Releases use semantic version tags such as `v0.1.0`; no source file needs a manual version edit.

`make build` injects the exact tag, Git commit, and UTC build time with Go linker flags. A build made from a commit without an exact tag reports `dev`. `dbf version` also reads Go's embedded VCS metadata as a fallback, including versions installed with `go install ...@vX.Y.Z`.

Create a release build from a clean tagged commit:

```bash
git tag -a v0.1.0 -m "debianform v0.1.0"
git push origin v0.1.0
make build
./dbf version
```

CI can provide explicit, reproducible metadata:

```bash
make build \
  VERSION=v0.1.0 \
  COMMIT="$(git rev-parse --short=12 HEAD)" \
  BUILD_DATE="2026-06-18T00:00:00Z"
```

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
- `debian_kernel_module`
- `debian_sysctl`
- `debian_nftables_file`

All resources use `host = "server1"`, where `server1` can be an SSH config `Host` alias. `dbf` always connects as `root`.

## Native System Configuration

These resources intentionally stay close to Debian's native files and commands.

```hcl
debian_kernel_module "br_netfilter" {
  host    = "server1"
  name    = "br_netfilter"
  persist = true
  path    = "/etc/modules-load.d/kubernetes.conf"
}

debian_sysctl "ip_forward" {
  host  = "server1"
  key   = "net.ipv4.ip_forward"
  value = "1"
  path  = "/etc/sysctl.d/99-kubernetes.conf"
}

debian_nftables_file "main" {
  host     = "server1"
  path     = "/etc/nftables.conf"
  validate = true
  activate = false

  content = <<-EOF
    flush ruleset

    table inet filter {
      chain input {
        type filter hook input priority 0; policy accept;
      }
    }
  EOF
}
```

`debian_kernel_module` runs `modprobe` and, when `persist = true`, writes a real `/etc/modules-load.d/*.conf` file.

`debian_sysctl` runs `sysctl -w` by default and, when `persist = true`, writes a real `/etc/sysctl.d/*.conf` file.

`debian_nftables_file` writes native nft syntax. With `validate = true`, it runs `nft -c -f` before installing the file. With `activate = true`, it runs `nft -f` after installing it.

## BBR Example

The complete BBR example is in [examples/bbr.dbf.hcl](examples/bbr.dbf.hcl). It loads `tcp_bbr`, persists the module, selects the `fq` queue discipline, and sets BBR as the TCP congestion control algorithm:

```bash
dbf plan -f examples/bbr.dbf.hcl
dbf apply -f examples/bbr.dbf.hcl
dbf check -f examples/bbr.dbf.hcl
```

Change the SSH host alias and state paths before using the example on another host.

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
