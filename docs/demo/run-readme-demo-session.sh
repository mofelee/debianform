#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="${DBF_DEMO_ROOT_DIR:?DBF_DEMO_ROOT_DIR is required}"
WORK_DIR="${DBF_DEMO_WORK_DIR:?DBF_DEMO_WORK_DIR is required}"
DBF_BIN="${DBF_DEMO_DBF_BIN:?DBF_DEMO_DBF_BIN is required}"
SSH_CONFIG="${DBF_DEMO_SSH_CONFIG:?DBF_DEMO_SSH_CONFIG is required}"
ALIAS="${DBF_DEMO_HOST_ALIAS:-demo1}"

TYPE_DELAY="${DBF_DEMO_TYPE_DELAY:-0.045}"
PAUSE_BEFORE_RUN="${DBF_DEMO_PAUSE_BEFORE_RUN:-1.5}"
PAUSE_AFTER_RUN="${DBF_DEMO_PAUSE_AFTER_RUN:-2.5}"
PAUSE_NOTE="${DBF_DEMO_PAUSE_NOTE:-1.2}"

cd "$WORK_DIR"
export DBF_SSH_CONFIG="$SSH_CONFIG"
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
  sleep "$PAUSE_BEFORE_RUN"
  "$@"
  sleep "$PAUSE_AFTER_RUN"
}

note() {
  local text=$1
  printf '\n'
  type_text "# $text"
  printf '\n'
  sleep "$PAUSE_NOTE"
}

show_file() {
  local file=$1
  printf '\n'
  type_text "$ sed -n '1,80p' $file"
  printf '\n'
  sleep "$PAUSE_BEFORE_RUN"
  sed -n '1,80p' "$file"
  sleep "$PAUSE_AFTER_RUN"
}

printf 'DebianForm: real apply on a disposable Debian host\n'
sleep "$PAUSE_AFTER_RUN"

note "Confirm the local dbf build."
run_cmd "dbf version" "$DBF_BIN" version

note "Confirm the target Debian host."
run_cmd "ssh $ALIAS 'grep PRETTY_NAME /etc/os-release; dpkg --print-architecture'" \
  "${SSH_BASE[@]}" "$ALIAS" "grep '^PRETTY_NAME=' /etc/os-release; dpkg --print-architecture"

note "Write one DebianForm config file."
cat >site.dbf.hcl <<EOF
host "demo1" {
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

note "Preview, apply, and verify no drift."
run_cmd "dbf validate" "$DBF_BIN" validate
run_cmd "dbf plan" "$DBF_BIN" plan --color always
run_cmd "dbf apply --auto-approve" "$DBF_BIN" apply --auto-approve --color always
run_cmd "dbf plan" "$DBF_BIN" plan --color always
run_cmd "dbf check" "$DBF_BIN" check --color always

note "Confirm the managed file on the target host."
run_cmd "ssh $ALIAS 'cat /etc/debianform-demo/message.txt'" \
  "${SSH_BASE[@]}" "$ALIAS" 'cat /etc/debianform-demo/message.txt'

printf '\n'
type_text "Done: plan, apply, no-op plan, and drift check all passed."
printf '\n'
