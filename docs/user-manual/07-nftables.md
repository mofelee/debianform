<p align="right">
  <strong>English</strong> | <a href="07-nftables.zh.md">简体中文</a>
</p>

# 07. Manage an nftables Firewall

This chapter installs and enables nftables, manages `/etc/nftables.conf` and a snippet file, and
revalidates and activates the ruleset after file drift.

The example has been verified on a Debian 13 amd64 test host. To avoid locking the operator out of
SSH, it uses `policy accept` and explicitly permits TCP port 22.

## Create a Working Directory

```bash
mkdir -p debianform-manual/07-nftables
cd debianform-manual/07-nftables
```

## Write the Configuration

Create `site.dbf.hcl`:

```hcl
host "manual1" {
  state {
    path      = "/var/lib/debianform/manual/07-state.json"
    lock_path = "/var/lock/debianform/manual/07-state.lock"
  }

  packages {
    install = ["nftables"]
  }

  nftables {
    enable = true

    main {
      content = <<-EOF
        flush ruleset
        include "/etc/nftables.d/debianform-manual-*.nft"
      EOF
    }

    file "10-input" {
      path = "/etc/nftables.d/debianform-manual-input.nft"

      content = <<-EOF
        table inet debianform_manual {
          chain input {
            type filter hook input priority 0; policy accept;

            ct state established,related accept
            iifname "lo" accept
            tcp dport 22 accept
            counter accept
          }
        }
      EOF
    }
  }
}
```

This configuration:

- Installs `nftables`.
- Writes `/etc/nftables.conf`.
- Writes `/etc/nftables.d/debianform-manual-input.nft`.
- Validates the rules with `nft -c -f /etc/nftables.conf`.
- Activates the rules with `nft -f /etc/nftables.conf`.
- Enables and starts `nftables.service`.

## Apply the Configuration

Run:

```bash
dbf validate
dbf plan --offline
dbf apply --auto-approve
```

The offline plan should contain four resources and two operations:

```text
Summary: 4 create, 0 update, 0 delete, 0 no-op, 2 operations
```

The two operations are:

```text
nft -c -f /etc/nftables.conf
nft -f /etc/nftables.conf
```

When files change, DebianForm validates first and activates second.

## Verify the Result

Run:

```bash
ssh manual1 'systemctl is-active nftables; systemctl is-enabled nftables; nft list ruleset | grep debianform_manual -A8'
```

Expected output includes:

```text
active
enabled
table inet debianform_manual {
  chain input {
    type filter hook input priority filter; policy accept;
    ct state established,related accept
    iifname "lo" accept
    tcp dport 22 accept
```

`nft list ruleset` may display `priority 0` as `priority filter`; that is nftables' output format.

## Introduce Ruleset-File Drift

Change the SSH port in the snippet from 22 to 2222:

```bash
ssh manual1 'sed -i "s/tcp dport 22 accept/tcp dport 2222 accept/" /etc/nftables.d/debianform-manual-input.nft'
```

Run:

```bash
dbf check
```

The command should fail:

```text
~ host.manual1.nftables.file["10-input"]
  update file /etc/nftables.d/debianform-manual-input.nft

Summary: 0 create, 1 update, 0 delete, 3 no-op, 2 operations
dbf: remote state does not match configuration
```

The summary still contains two operations. After an nftables file changes, DebianForm validates and
activates the ruleset again.

## Repair the Drift

Run:

```bash
dbf apply --auto-approve
dbf check
```

Confirm that both the file and live ruleset again permit TCP port 22:

```bash
ssh manual1 'grep -F "tcp dport 22 accept" /etc/nftables.d/debianform-manual-input.nft; nft list ruleset | grep -F "tcp dport 22 accept"'
```

Both lines should be present.

## Complete Chapter Command Sequence

```bash
mkdir -p debianform-manual/07-nftables
cd debianform-manual/07-nftables

cat > site.dbf.hcl <<'EOF'
host "manual1" {
  state {
    path      = "/var/lib/debianform/manual/07-state.json"
    lock_path = "/var/lock/debianform/manual/07-state.lock"
  }

  packages {
    install = ["nftables"]
  }

  nftables {
    enable = true

    main {
      content = <<-EOT
        flush ruleset
        include "/etc/nftables.d/debianform-manual-*.nft"
      EOT
    }

    file "10-input" {
      path = "/etc/nftables.d/debianform-manual-input.nft"

      content = <<-EOT
        table inet debianform_manual {
          chain input {
            type filter hook input priority 0; policy accept;

            ct state established,related accept
            iifname "lo" accept
            tcp dport 22 accept
            counter accept
          }
        }
      EOT
    }
  }
}
EOF

dbf validate
dbf plan --offline
dbf apply --auto-approve
ssh manual1 'systemctl is-active nftables; systemctl is-enabled nftables; nft list ruleset | grep debianform_manual -A8'

ssh manual1 'sed -i "s/tcp dport 22 accept/tcp dport 2222 accept/" /etc/nftables.d/debianform-manual-input.nft'
dbf check || true
dbf apply --auto-approve
dbf check
ssh manual1 'grep -F "tcp dport 22 accept" /etc/nftables.d/debianform-manual-input.nft; nft list ruleset | grep -F "tcp dport 22 accept"'
```

## Cleanup

To disable the configuration from this chapter:

```bash
ssh manual1 'systemctl disable --now nftables 2>/dev/null || true; rm -f /etc/nftables.conf /etc/nftables.d/debianform-manual-input.nft /var/lib/debianform/manual/07-state.json /var/lock/debianform/manual/07-state.lock; rm -rf /var/lock/debianform/manual/07-state.lock.d; nft flush ruleset 2>/dev/null || true'
```

You do not need to clean up before continuing to later chapters.
