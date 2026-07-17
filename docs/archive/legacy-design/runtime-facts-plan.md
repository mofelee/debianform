# DebianForm Runtime Facts Plan

<p align="right"><strong>English</strong> | <a href="runtime-facts-plan.zh.md">简体中文</a></p>

This document records the implementation plan for runtime facts such as
`system.architecture` and `system.codename`, and explicitly distinguishes a
desired hostname from an observed hostname.

## Goals

- Users normally do not need to declare `system.architecture` or
  `system.codename` in real deployment configuration.
- `system.hostname` is desired state; declaring it requests a remote hostname
  change.
- Online `plan`, `apply`, and `check` discover `architecture` and `codename` on
  the target and inject them into `target.system` before component instantiation.
- Online fact discovery may record the current remote hostname, but it is an
  observed `facts.system.hostname` and cannot override configured
  `system.hostname`.
- Explicitly declared `architecture` or `codename` values are assertions only;
  a mismatch with discovery must fail.
- `dbf validate` can check configuration and component-template structure
  without connecting to targets.
- `plan --offline` remains conservatively unavailable for configuration that
  requires runtime facts until an explicit fact-cache mechanism exists.

## Non-Goals

- Do not invent a default architecture or codename in the compiler.
- Do not substitute facts from the machine running DebianForm for target facts.
- Do not fabricate `target.system.architecture` or `target.system.codename`
  during validation to perform real source selection.
- Do not treat observed hostname as desired hostname, or desired hostname as a
  runtime fact.
- Do not implement a local fact cache in the first phase. Cache refresh,
  expiry, and consistency validation require a separate design.

## Design Principles

- Runtime facts can be trusted only when discovered from the target or
  explicitly declared as `architecture`/`codename` assertions.
- `system.hostname` belongs to desired system configuration;
  `facts.system.hostname` belongs to observed current state. Documentation, IR,
  state, and plan presentation must keep their namespaces distinct.
- Unknown facts may allow structural validation, but cannot drive a
  semantics-changing choice.
- Validation of a multi-architecture component should inspect every `source`
  branch instead of selecting a fabricated architecture.
- Online commands must recompile the complete configuration after discovery so
  `target.system.architecture`, `target.system.codename`, artifact sources, APT
  suites, and related fields come from the real target.
- Facts stored in state are observations from the last successful online run
  and must not silently become new defaults.

## Phase 1: Clarify Current Semantics

- Update user docs to describe `architecture` and `codename` as runtime facts.
- Explain that `system.hostname` is a desired hostname, defaults to the host
  label, and requests a remote hostname change when declared.
- Explain that explicit `architecture` and `codename` values are assertions,
  not required fields.
- Retain the existing online discovery sequence: compile basic host data,
  discover facts, then compile fully.
- Add mismatch coverage for explicit declarations versus discovered values.

Acceptance:

- A real host without `system.architecture` and `system.codename` can select a
  component source in online `plan`, `apply`, and `check`.
- When an explicit architecture or codename differs from discovery, the error
  identifies both values.
- `system.hostname` does not participate in fact-mismatch validation. Hostname
  convergence and drift belong to a separate system-configuration resource,
  not runtime-fact validation.

## Phase 2: Improve Validate

- Add a runtime-aware validation path.
- Validate neither connects over SSH nor requires runtime facts.
- Validate structural constraints across hosts, profiles, component inputs,
  component bodies, artifacts, services, systemd, APT, and related domains.
- For component artifacts:
  - Continue treating an unlabeled `source` as architecture-independent.
  - Validate URL, SHA-256, extraction, and installation rules for every labeled
    architecture source.
  - Continue rejecting mixed labeled and unlabeled sources.
- For expressions referencing `target.system.architecture` or
  `target.system.codename`, validate that the expression is recognizable as
  runtime-dependent without replacing the unknown with a fixed string.

Acceptance:

- A multi-architecture binary component without declared runtime facts passes
  `dbf validate`.
- Validate still finds an invalid SHA-256, extraction format, or missing install
  block in any architecture branch.
- An APT suite using `target.system.codename` does not require a handwritten
  codename during validation.
- `plan --offline` produces a clear error for configuration requiring runtime facts.

## Phase 3: Clean Up Examples and Integration Tests

- Remove unnecessary `system.architecture` and `system.codename` from real
  deployment examples.
- Retain `system.hostname` where the example should set the remote hostname and
  explain that it is desired state.
- Keep explicit runtime facts only in pure offline golden fixtures or fixtures
  that test assertion semantics intentionally.
- Update libvirt cases so online apply/check uses VM-discovered facts for
  architecture-specific source selection.
- Retain at least one mismatch test proving that explicit facts remain assertions.

Acceptance:

- Multi-architecture binary examples such as shadowsocks-rust need no
  handwritten `system.architecture`.
- Libvirt tests discover `amd64` and `trixie` automatically on Debian 13 and
  select the matching release asset.
- Layout-only validation no longer depends on fake facts in every case.

## Phase 4: Evaluate a Fact Cache

Design a fact cache only after fully offline planning becomes an explicit
requirement. At minimum, the cache design must answer:

- Cache-file location.
- Per-host separation and refresh.
- `detected_at` and expiry policy.
- Handling disagreement between cache and remote discovery online.
- How users view, refresh, and delete the cache.

Until such a design exists, `plan --offline` must not borrow facts implicitly
from state or the local environment.

## Risks

- Fabricated facts during validation can hide errors in other architecture branches.
- Using local-machine facts offline selects incorrect artifacts or APT suites
  for cross-deployment.
- Treating state facts as current silently retains an old codename after a system upgrade.
- Blurring `system.hostname` and `facts.system.hostname` can make users think a
  configured hostname is an assertion rather than desired state that mutates the target.
- When explicit `architecture` or `codename` looks simultaneously like
  configuration and facts, users may think DebianForm can change architecture
  or release codename.

One principle addresses all of these risks: unknown means unknown. Validate may
accept an unknown, but cannot treat it as fact.
