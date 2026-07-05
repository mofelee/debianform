component "watched_config" {
  input "message" {
    type = string
  }

  script "record_change" {
    mode = "once"

    content = <<-EOF
      set -eu
      mkdir -p /var/lib/debianform-script-on-change
      count_file=/var/lib/debianform-script-on-change/reload.count
      count=0
      if [ -f "$count_file" ]; then
        count="$(cat "$count_file")"
      fi
      count="$((count + 1))"
      printf '%s\n' "$count" > "$count_file"
      printf '%s\n' "$DBF_SCRIPT_NAME" > /var/lib/debianform-script-on-change/script.name
      printf '%s\n' "$DBF_COMPONENT_NAME" > /var/lib/debianform-script-on-change/component.name
      printf '%s\n' "$DBF_TRIGGER_PATH" > /var/lib/debianform-script-on-change/trigger.path
      printf '%s\n' "$DBF_TRIGGER_PATHS" > /var/lib/debianform-script-on-change/trigger.paths
    EOF
  }

  files {
    file "/etc/debianform-script-on-change/app.env" {
      owner = "root"
      group = "root"
      mode  = "0644"

      content = "MESSAGE=${input.message}\n"

      on_change = script.record_change
    }
  }
}

host "cihost" {
  ssh {
    host          = "__DBF_VM_IP__"
    user          = "root"
    identity_file = "${path.module}/id_ed25519"
  }

  state {
    path      = "/var/lib/debianform-integration/script-on-change-state.json"
    lock_path = "/var/lock/debianform-integration/script-on-change-state.lock"
  }

  component "app" {
    source = component.watched_config

    inputs = {
      message = "hello"
    }
  }
}
