component "service_unit_fixture" {
  input "structured_extra" {
    type     = string
    nullable = false
  }

  input "service_enabled" {
    type     = bool
    nullable = false
  }

  input "service_state" {
    type     = string
    nullable = false
  }

  directories {
    directory "/var/lib/debianform-service-unit" {
      owner = "root"
      group = "root"
      mode  = "0755"
    }
  }

  files {
    file "/usr/local/bin/dbf-service-unit-worker" {
      owner = "root"
      group = "root"
      mode  = "0755"

      content = <<-EOF
        #!/usr/bin/env sh
        set -eu
        name="$${1:?service name required}"
        runtime_dir=/run/debianform-service-unit
        mkdir -p "$runtime_dir"
        starts_file="$runtime_dir/$name.starts"
        starts=0
        if [ -f "$starts_file" ]; then
          starts="$(cat "$starts_file")"
        fi
        printf '%s\n' "$((starts + 1))" > "$starts_file"
        printf '%s\n' "$name" > "$runtime_dir/$name.name"
        printf '%s\n' "$${DBF_SERVICE_MODE:-}" > "$runtime_dir/$name.mode"
        printf '%s\n' "$${DBF_EXTRA:-}" > "$runtime_dir/$name.extra"
        exec sleep infinity
      EOF
    }
  }

  systemd {
    unit "dbf-raw.service" {
      content = <<-EOF
        [Unit]
        Description=DebianForm Raw Service Unit
        Wants=network-online.target
        After=network-online.target

        [Service]
        Environment=DBF_SERVICE_MODE=raw
        ExecStart=/usr/local/bin/dbf-service-unit-worker raw
        Restart=always
        RestartSec=1s
        StandardOutput=journal
        StandardError=journal

        [Install]
        WantedBy=multi-user.target
      EOF
    }

    service_unit "dbf-structured" {
      description = "DebianForm Structured Service Unit"

      run = [
        "/usr/local/bin/dbf-service-unit-worker",
        "structured",
      ]

      restart       = "always"
      restart_delay = "1s"
      change_action = "restart"
      wants         = ["network-online.target"]
      after         = ["network-online.target"]
      stdout        = "journal"
      stderr        = "journal"

      environment = {
        DBF_EXTRA        = input.structured_extra
        DBF_SERVICE_MODE = "structured"
      }
    }
  }

  services {
    service "dbf-raw" {
      enabled = input.service_enabled
      state   = input.service_state
    }

    service "dbf-structured" {
      enabled = input.service_enabled
      state   = input.service_state
    }
  }
}
