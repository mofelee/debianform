#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="${DBF_DEMO_ROOT_DIR:?DBF_DEMO_ROOT_DIR is required}"
WORK_DIR="${DBF_DEMO_WORK_DIR:?DBF_DEMO_WORK_DIR is required}"
DBF_BIN="${DBF_DEMO_DBF_BIN:?DBF_DEMO_DBF_BIN is required}"
SSH_CONFIG="${DBF_DEMO_SSH_CONFIG:?DBF_DEMO_SSH_CONFIG is required}"
ALIAS="${DBF_DEMO_HOST_ALIAS:-dbf-demo-host}"

TYPE_DELAY="${DBF_DEMO_TYPE_DELAY:-0.018}"
PAUSE_SHORT="${DBF_DEMO_PAUSE_SHORT:-0.35}"
PAUSE_LONG="${DBF_DEMO_PAUSE_LONG:-0.75}"

cd "$WORK_DIR"
export DBF_SSH_CONFIG="$SSH_CONFIG"
export NO_COLOR=1
export TERM="${TERM:-xterm-256color}"
SSH_BASE=(ssh -F "$SSH_CONFIG" -o BatchMode=yes -o ConnectTimeout=10)

type_text() {
  local text=$1
  local i char
  for ((i = 0; i < ${#text}; i++)); do
    char=${text:i:1}
    printf '%s' "$char"
    sleep "$TYPE_DELAY"
  done
}

run_cmd() {
  local display=$1
  shift
  printf '\n'
  type_text "$ $display"
  printf '\n'
  sleep "$PAUSE_SHORT"
  "$@"
  sleep "$PAUSE_LONG"
}

show_file() {
  local file=$1
  printf '\n'
  type_text "$ sed -n '1,80p' $file"
  printf '\n'
  sleep "$PAUSE_SHORT"
  sed -n '1,80p' "$file"
  sleep "$PAUSE_LONG"
}

printf 'DebianForm: real apply on a disposable Debian host\n'
sleep "$PAUSE_LONG"

run_cmd "dbf version" "$DBF_BIN" version
run_cmd "ssh $ALIAS 'grep PRETTY_NAME /etc/os-release; dpkg --print-architecture'" \
  "${SSH_BASE[@]}" "$ALIAS" "grep '^PRETTY_NAME=' /etc/os-release; dpkg --print-architecture"

cat >site.dbf.hcl <<EOF
host "demo1" {
  ssh {
    host = "$ALIAS"
  }

  directories {
    directory "/etc/debianform-demo" {
      owner = "root"
      group = "root"
      mode  = "0755"
    }
  }

  files {
    file "/etc/debianform-demo/message.txt" {
      owner   = "root"
      group   = "root"
      mode    = "0644"
      content = "managed by DebianForm\\n"
    }
  }
}
EOF

show_file site.dbf.hcl

run_cmd "dbf validate -f site.dbf.hcl" "$DBF_BIN" validate -f site.dbf.hcl
run_cmd "dbf plan -f site.dbf.hcl" "$DBF_BIN" plan -f site.dbf.hcl --color never
run_cmd "dbf apply -f site.dbf.hcl --auto-approve" "$DBF_BIN" apply -f site.dbf.hcl --auto-approve --color never
run_cmd "dbf plan -f site.dbf.hcl" "$DBF_BIN" plan -f site.dbf.hcl --color never
run_cmd "dbf check -f site.dbf.hcl" "$DBF_BIN" check -f site.dbf.hcl --color never
run_cmd "ssh $ALIAS 'cat /etc/debianform-demo/message.txt'" \
  "${SSH_BASE[@]}" "$ALIAS" 'cat /etc/debianform-demo/message.txt'

printf '\n'
type_text "Done: plan, apply, no-op plan, and drift check all passed."
printf '\n'
