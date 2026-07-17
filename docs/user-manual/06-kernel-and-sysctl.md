<p align="right">
  <strong>English</strong> | <a href="06-kernel-and-sysctl.zh.md">简体中文</a>
</p>

# 06. Manage Kernel Modules, sysctl, and BBR

This chapter loads and persists a kernel module and configures sysctl, using BBR as a real example.
The example changes runtime network settings on the test host:

- `net.core.default_qdisc = fq`
- `net.ipv4.tcp_congestion_control = bbr`

Run it only on a low-risk test host. The example has been verified on Debian 13 amd64.

## Create a Working Directory

```bash
mkdir -p debianform-manual/06-kernel-and-sysctl
cd debianform-manual/06-kernel-and-sysctl
```

## Write the Configuration

Create `site.dbf.hcl`:

```hcl
host "manual1" {
  state {
    path      = "/var/lib/debianform/manual/06-state.json"
    lock_path = "/var/lock/debianform/manual/06-state.lock"
  }

  kernel {
    modules = [
      "tcp_bbr",
    ]

    sysctl = {
      "net.core.default_qdisc"          = "fq"
      "net.ipv4.tcp_congestion_control" = "bbr"
    }
  }

  assert {
    condition = contains(self.kernel.modules, "tcp_bbr")
    message   = "BBR requires the tcp_bbr kernel module."
  }
}
```

`kernel.modules` loads the module and writes `/etc/modules-load.d/dbf-tcp_bbr.conf`.

By default, `kernel.sysctl` does both of the following:

- Applies the runtime value with `sysctl -w`.
- Persists it under `/etc/sysctl.d/99-dbf-*.conf`.

The `assert` is a configuration self-check. If someone later removes the `tcp_bbr` module
declaration, `validate` fails.

## Apply the Configuration

Run:

```bash
dbf validate
dbf plan --offline
dbf apply --auto-approve
```

The first online apply may show three updates rather than creates:

```text
Summary: 0 create, 3 update, 0 delete, 0 no-op, 0 operations
```

This is expected. The sysctl keys already exist in the kernel; DebianForm changes their current
values to the desired values and writes persistent files.

## Verify the Result

Run:

```bash
ssh manual1 'awk "$1 == \"tcp_bbr\" { found = 1 } END { exit found ? 0 : 1 }" /proc/modules; sysctl -n net.core.default_qdisc; sysctl -n net.ipv4.tcp_congestion_control; cat /etc/modules-load.d/dbf-tcp_bbr.conf; cat /etc/sysctl.d/99-dbf-net_core_default_qdisc.conf; cat /etc/sysctl.d/99-dbf-net_ipv4_tcp_congestion_control.conf'
```

Expected output includes:

```text
fq
bbr
tcp_bbr
net.core.default_qdisc = fq
net.ipv4.tcp_congestion_control = bbr
```

The first `awk` command has no output; its exit status confirms that `tcp_bbr` is loaded.

## Introduce sysctl Drift

Change TCP congestion control back to `cubic` manually:

```bash
ssh manual1 'sysctl -w net.ipv4.tcp_congestion_control=cubic'
```

Run:

```bash
dbf check
```

The command should fail:

```text
~ host.manual1.kernel.sysctl["net.ipv4.tcp_congestion_control"]
  sysctl -w net.ipv4.tcp_congestion_control=bbr and write /etc/sysctl.d/99-dbf-net_ipv4_tcp_congestion_control.conf

Summary: 0 create, 1 update, 0 delete, 2 no-op, 0 operations
dbf: remote state does not match configuration
```

## Repair the Drift

Run:

```bash
dbf apply --auto-approve
dbf check
```

Confirm that the value was restored:

```bash
ssh manual1 'sysctl -n net.ipv4.tcp_congestion_control'
```

Expected output:

```text
bbr
```

## Complete Chapter Command Sequence

```bash
mkdir -p debianform-manual/06-kernel-and-sysctl
cd debianform-manual/06-kernel-and-sysctl

cat > site.dbf.hcl <<'EOF'
host "manual1" {
  state {
    path      = "/var/lib/debianform/manual/06-state.json"
    lock_path = "/var/lock/debianform/manual/06-state.lock"
  }

  kernel {
    modules = [
      "tcp_bbr",
    ]

    sysctl = {
      "net.core.default_qdisc"          = "fq"
      "net.ipv4.tcp_congestion_control" = "bbr"
    }
  }

  assert {
    condition = contains(self.kernel.modules, "tcp_bbr")
    message   = "BBR requires the tcp_bbr kernel module."
  }
}
EOF

dbf validate
dbf plan --offline
dbf apply --auto-approve
ssh manual1 'awk "$1 == \"tcp_bbr\" { found = 1 } END { exit found ? 0 : 1 }" /proc/modules; sysctl -n net.core.default_qdisc; sysctl -n net.ipv4.tcp_congestion_control; cat /etc/modules-load.d/dbf-tcp_bbr.conf; cat /etc/sysctl.d/99-dbf-net_core_default_qdisc.conf; cat /etc/sysctl.d/99-dbf-net_ipv4_tcp_congestion_control.conf'

ssh manual1 'sysctl -w net.ipv4.tcp_congestion_control=cubic'
dbf check || true
dbf apply --auto-approve
dbf check
ssh manual1 'sysctl -n net.ipv4.tcp_congestion_control'
```

## Cleanup or Rollback

To remove this chapter's state and persistent files and restore runtime congestion control to
`cubic`:

```bash
ssh manual1 'rm -f /var/lib/debianform/manual/06-state.json /var/lock/debianform/manual/06-state.lock /etc/modules-load.d/dbf-tcp_bbr.conf /etc/sysctl.d/99-dbf-net_core_default_qdisc.conf /etc/sysctl.d/99-dbf-net_ipv4_tcp_congestion_control.conf; rm -rf /var/lock/debianform/manual/06-state.lock.d; sysctl -w net.core.default_qdisc=fq_codel >/dev/null || true; sysctl -w net.ipv4.tcp_congestion_control=cubic >/dev/null || true'
```

You do not need to clean up before continuing to later chapters.
