# DebianForm WireGuard Example Plan

<p align="right"><strong>English</strong> | <a href="wireguard-example-plan.zh.md">简体中文</a></p>

This document records the implementation direction for the current WireGuard
example and two-host integration test. WireGuard is managed through native
systemd-networkd `.netdev` / `.network` files, without installing
`wireguard-tools` or using `wg-quick`.

## Goals

- Generate native networkd configuration through the `systemd.networkd` DSL.
- Expose `interface.route_table` as a structured component input defaulting to
  `"off"`. This generates `RouteTable=off`, preventing networkd from adding
  system routes automatically from peer `AllowedIPs`; users manage routes
  explicitly.
- Make the component reusable for multiple WireGuard interfaces on one machine.
  Interface name, networkd file paths, and private-key path cannot be fixed to
  `wg0` or `/etc/wireguard/private.key`.
- Support several peers on one interface. Callers identify peers with stable
  labels, and component expansion produces multiple `[WireGuardPeer]` sections.
- Write the WireGuard private key through `secrets.file` to
  `/etc/wireguard/${input.interface.name}.key`.
- Use `root:systemd-network 0640` on the private-key file because
  `systemd-networkd.service` runs as `systemd-network` on Debian.
- Use `PrivateKeyFile` in `.netdev`; forbid inline `PrivateKey`.
- Create two Debian VMs in integration and verify bidirectional tunnel ping on `wg0`.

## Reuse Requirements

The original example coupled `netdev "10-wg0"`, `network "20-wg0"`,
`Name = "wg0"`, and `/etc/wireguard/private.key`. A machine connecting to two
WireGuard networks therefore had to duplicate the component or write networkd
manually. Mounting the component twice on one host also caused collisions on
the private-key path and networkd block labels.

The new component API uses:

- `interface.name` as the real interface name, such as `wg-prod` or `wg-backup`.
- Explicit `path = "/etc/systemd/network/10-${input.interface.name}.netdev"`
  and `path = "/etc/systemd/network/20-${input.interface.name}.network"`.
- `secrets.file "private_key" { path = "/etc/wireguard/${input.interface.name}.key" }`
  to prevent component instances from overwriting one key.
- `peers = map(object(...))` for multi-peer input. The map key is the peer label
  used for stable `[WireGuardPeer]` ordering and error locations.

Caller example:

```hcl
component "wg_prod" {
  source = component.wireguard_networkd

  inputs = {
    private_key_source = "secrets/edge1-prod.key"
    interface = {
      name    = "wg-prod"
      address = "10.80.0.10/24"
    }
    peers = {
      server_a = {
        public_key  = "<server-a-public-key>"
        allowed_ips = ["10.80.0.1/32", "10.10.0.0/16"]
        endpoint    = "server-a.example.net:51820"
      }
      server_b = {
        public_key  = "<server-b-public-key>"
        allowed_ips = ["10.80.0.2/32", "10.20.0.0/16"]
        endpoint    = "server-b.example.net:51820"
      }
    }
  }
}
```

## Implementation Loops

### Loop 1: networkd Peer Map

Let `systemd.networkd.netdev` accept a `wireguard_peer = <map>` attribute. This
attribute and existing `wireguard_peer "<label>" { ... }` blocks use the same
internal representation: map key is the peer label, and map value is the
networkd section. Retain the block form for compatibility.

Acceptance:

- `wireguard_peer = { server = { PublicKey = "...", AllowedIPs = [...] } }`
  compiles.
- Existing `wireguard_peer "server" { ... }` remains valid.
- One `netdev` cannot define the same peer through both attribute and block.
- Inline `PresharedKey` remains forbidden; use `PresharedKeyFile`.

### Loop 2: Reusable Component Example

Upgrade `examples/wireguard-networkd.dbf.hcl`:

- Replace the `peer` input with `peers = map(object(...))`.
- Convert `input.peers` into a networkd `WireGuardPeer` section map inside the component.
- Use generic netdev/network block labels and bind their paths to
  `input.interface.name`.
- Write the private key to `/etc/wireguard/${input.interface.name}.key`.
- Show at least one host with multiple peers and another with only one.

### Loop 3: Duplicate Example Cleanup

`examples/wireguard-networkd.dbf.hcl` and
`examples/systemd-networkd-wireguard.dbf.hcl` were identical. Retain
`examples/wireguard-networkd.dbf.hcl` as the sole runnable WireGuard component
example, remove the duplicate, and update README, support-matrix, and this
document's references.

## Current Implementation

- `systemd { networkd { ... } }` supports:
  - `netdev "<label>"` -> `/etc/systemd/network/<label>.netdev`
  - `network "<label>"` -> `/etc/systemd/network/<label>.network`
  - `wireguard_peer "<label>"` or `wireguard_peer = { ... }` -> repeated
    `[WireGuardPeer]` sections
- Networkd sections use a map/list structure close to the native format:
  - A scalar generates `Key=value`.
  - A list generates repeated `Key=value` entries.
- The graph compiles them into file-like resources and, after configuration changes, runs:
  - `systemctl start systemd-networkd.service`
  - `networkctl reload`
  - For an absent netdev, additionally run `ip link delete <Name> ... || true`.

## Example

Primary example:

```text
examples/wireguard-networkd.dbf.hcl
```

Before real validation/apply, prepare locally:

```text
examples/secrets/wg-a.key
examples/secrets/wg-b.key
```

`examples/secrets/` is gitignored. Never commit production private keys.

## Integration Test

Case path:

```text
test/integration/libvirt/cases/wireguard/
```

Topology:

- VM A libvirt IP: `__DBF_WG_A_VM_IP__`.
- VM B libvirt IP: `__DBF_WG_B_VM_IP__`.
- WireGuard A tunnel IP: `10.80.0.1/30`.
- WireGuard B tunnel IP: `10.80.0.2/30`.
- UDP port: `51820`.

Test steps:

- Step 1: Write private keys, `.netdev`, and `.network` files.
- Step 2: Start/reload networkd and verify `wg0` addresses and bidirectional ping.
- Step 3: Remove A's `.netdev` to create drift, assert `dbf check` fails, and repair with apply.
- Step 4: Declare networkd files absent, remove `wg0`, remove networkd file
  resources from state, and retain the private key.
- Step 5: Remove the component and destroy private keys, networkd files, and
  state resources.

Key assertions:

- `wireguard-tools` is not installed.
- `wg-quick@wg0.service` does not exist.
- State contains no plaintext test private key.
- A/B tunnel ping succeeds in both directions.
- Drift is detected and repaired.

## Verification Commands

```bash
go test ./internal/core/parser ./internal/core/merge ./internal/core/graph ./internal/core/plan ./internal/core/engine ./cmd/dbf
make test-integration-layout
make test-integration-case CASE=wireguard
```
