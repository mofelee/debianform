host "cihost" {
  ssh {
    host          = "__DBF_VM_IP__"
    user          = "root"
    identity_file = "${path.module}/id_ed25519"
  }

  state {
    path      = "/var/lib/debianform-integration/service-unit-state.json"
    lock_path = "/var/lock/debianform-integration/service-unit-state.lock"
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
        mkdir -p /run/debianform-service-unit
        printf '%s\n' "$name" > "/run/debianform-service-unit/$name.name"
        printf '%s\n' "$${DBF_SERVICE_MODE:-}" > "/run/debianform-service-unit/$name.mode"
        printf '%s\n' "$${DBF_EXTRA:-}" > "/run/debianform-service-unit/$name.extra"
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
      wants         = ["network-online.target"]
      after         = ["network-online.target"]
      stdout        = "journal"
      stderr        = "journal"

      environment = {
        DBF_EXTRA        = "from-structured"
        DBF_SERVICE_MODE = "structured"
      }
    }
  }

  services {
    service "dbf-raw" {
      enabled = true
      state   = "running"
    }

    service "dbf-structured" {
      enabled = true
      state   = "running"
    }
  }
}
