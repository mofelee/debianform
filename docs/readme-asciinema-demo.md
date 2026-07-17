<p align="right">
  <strong>English</strong> | <a href="readme-asciinema-demo.zh.md">简体中文</a>
</p>

# Recording the README asciinema Demo

This document describes how to generate the terminal demo shown in the GitHub README. The README
embeds `docs/demo/debianform-quickstart.svg`, while the reproducible asciinema source recording is
retained as `docs/demo/debianform-quickstart.cast`.

## Dependencies

- `asciinema` to record the `.cast` file.
- `node` and `npx` to render an animated SVG with `svg-term-cli`.
- `go` to build `dbf` from the current checkout.
- `virsh`, `ssh`, and
  `/root/.codex/skills/virsh-test-host/scripts/virsh-test-host.sh`.
- A working libvirt URI. In the current environment this is usually
  `LIBVIRT_DEFAULT_URI=qemu+ssh://ks/system`.

The recording script creates and destroys one temporary VM with the fixed name
`dbf-test-readme-demo`. Cleanup is scoped to that domain name with the `dbf-test` prefix.

## Recording Requirements

- Print a short `# ...` comment before each command to explain the next action.
- Use a deliberately slow typing speed. The default is `DBF_DEMO_TYPE_DELAY=0.045`, so the README
  animation remains readable.
- Pause after typing each command and before running it. The default is
  `DBF_DEMO_PAUSE_BEFORE_RUN=1.5`. Pause after command output as well; the default is
  `DBF_DEMO_PAUSE_AFTER_RUN=2.5`.
- Preserve color. Use `--color always` for `plan`, `apply`, and `check`, and do not set `NO_COLOR`.
- Generate only one `site.dbf.hcl` in the recording directory. Run
  `dbf validate/plan/apply/check` without `-f` so the demonstration matches the beginner
  Quickstart.

## Generation Procedure

From the repository root, run:

```bash
docs/demo/record-readme-demo.sh
docs/demo/render-readme-demo.sh
```

The first command:

- Builds a temporary `dbf` binary with `go build -buildvcs=false`, preventing the recorded working
  tree's `+dirty` state from appearing in the README demo.
- Uses `virsh-test-host` to create a temporary Debian 13 host.
- Generates a temporary SSH config outside the recording and shows only the stable alias `demo1`
  on screen. The temporary config begins with `Include ~/.ssh/config`, so local jump-host aliases
  such as `ProxyJump ks` still resolve.
- Records local `dbf version`, the remote Debian version and architecture, `validate`, an online
  `plan`, a real `apply`, the second no-op `plan`, and `check`.
- Automatically destroys `dbf-test-readme-demo` on exit.

The second command renders the `.cast` file as the SVG used by the README.

## Tunable Parameters

Common environment variables:

```bash
DBF_DEMO_DOMAIN=dbf-test-readme-demo
DBF_DEMO_HOST_ALIAS=demo1
DBF_DEMO_COLS=90
DBF_DEMO_ROWS=28
DBF_DEMO_IDLE_TIME_LIMIT=2
DBF_DEMO_TYPE_DELAY=0.045
DBF_DEMO_PAUSE_BEFORE_RUN=1.5
DBF_DEMO_PAUSE_AFTER_RUN=2.5
DBF_DEMO_PAUSE_NOTE=1.2
DBF_TEST_POOL=vm
DBF_TEST_NETWORK=default
```

If the temporary host was not cleaned up automatically, remove it manually:

```bash
DBF_TEST_NAME=dbf-test-readme-demo \
  /root/.codex/skills/virsh-test-host/scripts/virsh-test-host.sh destroy dbf-test-readme-demo
```

## Prepublication Checks

```bash
asciinema cat docs/demo/debianform-quickstart.cast >/dev/null
test -s docs/demo/debianform-quickstart.svg
asciinema cat docs/demo/debianform-quickstart.cast | rg '# Confirm|# Write|# Preview'
asciinema cat docs/demo/debianform-quickstart.cast | rg --fixed-strings $'\033['
! asciinema cat docs/demo/debianform-quickstart.cast | rg --fixed-strings -- '-f site.dbf.hcl'
! rg '192\\.168\\.|ProxyJump|IdentityFile|/root/\\.ssh' docs/demo/debianform-quickstart.cast docs/demo/debianform-quickstart.svg
```

The final two negated checks must produce no output. If they find `-f`, a local IP address, a jump
host, or a private-key path, update the scripts and record the demo again before publishing it.
