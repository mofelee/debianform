<p align="right">
  <strong>English</strong> | <a href="networkd-generalization-contract.zh.md">简体中文</a>
</p>

# systemd.networkd generalization contract

Status: implementation contract for the Preview `systemd.networkd` provider.

This document locks the DSL and lifecycle decisions for issue #72. The feature remains Preview;
these additions do not make networkd management Stable or allow DebianForm to take ownership from
Netplan or NetworkManager.

## DSL

Each `netdev` and `network` resource supports one of three content forms:

- compatibility attributes (`netdev`, `wireguard`, `wireguard_peer`, `match`, and `network`);
- generic `section "<identity>"` blocks; or
- exactly one raw `content` or `source` attribute.

The forms are mutually exclusive. `source` is resolved relative to the declaring `.dbf.hcl` file,
as it is for `files.file`. Raw content can be marked with `sensitive = true`; content derived from a
sensitive value is marked automatically.

```hcl
network "wg-peer" {
  section "match" {
    name = "Match"
    settings = {
      Name = "wg-peer"
    }
  }

  section "ipv4" {
    name = "Address"
    settings = {
      Address        = "10.2.0.0/31"
      AddPrefixRoute = false
    }
  }

  activation {
    reconfigure = ["wg-peer"]
    post_reload = script.reexport_bird
  }
}
```

The block label is a DebianForm-local stable identity and is not rendered. `name` is the actual
networkd section name. Generic section blocks render in declaration order. Setting keys render in
lexical order, while list values render as repeated keys in list order. Null settings are omitted.
Compatibility attributes retain their existing section and key ordering, so lowering them into the
shared representation does not change existing file bytes.

Section names and setting keys must be non-empty, single-line systemd identifiers. Scalar values
must be strings, numbers, or booleans and must not contain NUL or newline characters. Booleans render
as `yes` or `no`. Lists contain only those scalar types. Syntactically valid unknown section names and
keys are accepted.

A present structured netdev must contain one `[NetDev]` section with non-empty `Name` and `Kind`.
A present raw netdev is checked for the same fields without otherwise imposing a closed networkd
schema. Inline `PrivateKey` or `PresharedKey` is accepted only when its expression carries a sensitive
mark; file-backed key directives remain recommended. Ephemeral settings and raw content are rejected
because networkd resources do not have a write-only content-version contract.

## Activation and identity

`activation.reconfigure` is an ordered list of interface names.
`activation.post_reload` accepts `script.<name>` or `global.script.<name>` with the same resolution and
declaration-identity rules as component `files.file.on_change`. References resolve during validation.
A root script must use `mode = "once"`.

For each apply, changed networkd resources contribute to one host activation chain:

1. write or remove all changed managed files;
2. ensure `systemd-networkd.service` is running when requested;
3. run one `networkctl reload`;
4. remove runtime links for deleted netdev resources;
5. run one deterministic `networkctl reconfigure <interface>` operation per affected interface;
6. run each affected post-reload script once.

Interface names are unioned and sorted. A post-reload declaration is deduplicated by declaration
identity, not command text. Different declarations remain different operations. Reconfigure and
post-reload operations depend on reload, and post-reload operations also depend on all affected
reconfigure operations. No changed trigger means no activation operation. `check` remains
observational; offline plan still shows the declared operation graph.

The existing `networkd_netdev` and `networkd_network` resource kinds and addresses remain unchanged.
Generic sections and raw content are content details, not state resources. Raw resources retain the
networkd ownership preflight, lifecycle protection, desired hashing, check/drift behavior, reload
aggregation, and netdev runtime cleanup.

## Migration boundary

Changing an existing native networkd resource from compatibility syntax to equivalent generic
sections is an in-place content update and must be reviewed for byte equality before apply. Changing a
`files.file` declaration into a native networkd resource is not an automatic state move: operators
must back up state and network files, verify exclusive path ownership, plan from a local console or
other recovery path, and retain a rollback configuration. DebianForm does not infer ownership or
perform an unreviewed runtime interface deletion.
