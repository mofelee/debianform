<p align="right">
  <strong>English</strong> | <a href="README.zh.md">简体中文</a>
</p>

# DebianForm Documentation

This is the documentation hub for DebianForm. Start with the Quickstart if you are new to the
project, use the CLI Manual for command details, and consult the Operations Runbook when
troubleshooting.

## Getting Started

- [Quickstart](quickstart.md): the shortest path from installation to your first `apply` and `check`.
- [Ubuntu 24.04 Preview Quickstart](ubuntu-24.04-quickstart.md): the initial root SSH,
  platform-fact, plan/apply/no-op/check workflow for Ubuntu 24.04 LTS amd64.
- [Ubuntu 26.04 Preview Quickstart](ubuntu-26.04-quickstart.md): the initial root SSH and
  plan/apply/no-op/check workflow for the complete Ubuntu 26.04 LTS amd64 `resolute` tuple.
- [User Manual](user-manual/README.md): progressive, runnable tutorials for common operations.
- [CLI Manual](cli.md): `validate`, `plan`, `apply`, `check`, `fmt`, and inspection commands.
- [DSL Reference](dsl-reference.md): implemented `.dbf.hcl` declarations, fields, defaults,
  constraints, and testable examples.
- [Realistic Deployment Template](realistic-deployment-example.md): a complete, compact example
  of a least-privilege systemd application.
- [README asciinema Recording Guide](readme-asciinema-demo.md): regenerate the terminal demo on
  the GitHub project page.

## Everyday Reference

- [Support Matrix](support-matrix.md): supported platforms, configuration blocks, resource types,
  and example status.
- [Ubuntu 24.04 Support Contract](ubuntu-24.04-support-contract.md): Preview scope, exclusions,
  the Netplan ownership boundary, and the independent gate for Ubuntu 24.04.
- [Ubuntu 26.04 Support Contract](ubuntu-26.04-support-contract.md): the Preview tuple, release
  differences, released-image evidence, and the four managed-target matrices.
- [Security Model](security-model.md): root SSH, secret redaction, state and lock boundaries, and
  vulnerability response.
- [Compatibility Policy](compatibility-policy.md): CLI, DSL, state, and plan JSON compatibility
  rules across beta and stable releases.
- [How DebianForm Works](how-it-works/README.md): a developer-oriented series about the internal
  architecture and implementation flow.
- [Plan JSON Format](plan-format.md): structured output from `dbf plan --format json`.
- [State Format](state.md): remote state, locking, ownership, and redaction rules.
- [systemd Service Units](systemd-service-units.md): plain-text units and structured
  `service_unit` declarations.
- [script / on_change](script-on-change-requirements.md): script-hook semantics for changes to
  files inside a component.
- [CLI Color and Logging Policy](cli-color-output-policy.md): terminal and log colors, CI behavior,
  and JSON output boundaries.
- [Delete-Behavior Diagnostics Design](delete-behavior-diagnostics-plan.md): plan/apply deletion
  messages, colors, and the behavior matrix.
- [Documentation Localization Policy](localization-policy.md): bilingual file naming, translation,
  navigation, and validation requirements.

## Requirements and Implementation Plans

- [script / on_change Implementation Plan](script-on-change-implementation-plan.md): executable
  development loops for the change-hook feature.

## Operations and Troubleshooting

- [Operations Runbook](operations-runbook.md): stale locks, interrupted applies, drift, resource
  recovery, and common errors.
- [Platform Support Strategy](platform-support-strategy.md): supported Debian versions and
  architectures, plus promotion criteria for each support tier.

## Release Maintenance

- [Release Quick Runbook](release-quick-runbook.md): the routine release procedure.
- [Release Process](release-process.md): artifacts, release gates, installation, and upgrades.
- [Release Notes Template](release-notes-template.md): the required GitHub Release structure.
- [Release Automation Plan](release-automation-plan.md): implementation record for the release
  workflow.
- [Linux Homebrew Verification Policy](linux-homebrew-verification-policy.md): the best-effort
  boundary for Homebrew on Linux.
- [APT Repository Feasibility](apt-repository-feasibility.md): the prospective path for `.deb`
  packages and an APT repository.
- [Beta Feedback Triage](beta-feedback-triage.md): beta feedback channels and triage rules.

## Archived Designs

Historical requirements, implementation plans, and obsolete checklists live under
[`archive/legacy-design/`](archive/legacy-design/). They exist only to preserve design history and
must not be treated as current user guidance or capability commitments.
