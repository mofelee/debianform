host "cihost" {
  ssh {
    host          = "__DBF_VM_IP__"
    user          = "root"
    identity_file = "${path.module}/id_ed25519"
  }

  state {
    path      = "/var/lib/debianform-integration/systemd-extensions-state.json"
    lock_path = "/var/lock/debianform-integration/systemd-extensions-state.lock"
  }

  packages {
    install = ["systemd-resolved"]
  }

  files {
    file "/usr/local/bin/dbf-systemd-extensions-worker" {
      owner = "root"
      group = "root"
      mode  = "0755"

      content = <<-EOF
        #!/usr/bin/env sh
        set -eu
        marker_dir=/run/debianform-systemd-extensions
        count_file="$marker_dir/timer-count"
        mkdir -p "$marker_dir"
        count=0
        if [ -f "$count_file" ]; then
          count="$(cat "$count_file")"
        fi
        printf '%s\n' "$((count + 1))" > "$count_file"
        printf 'v2\n' > "$marker_dir/version"
        date +%s > "$marker_dir/timer-ran"
      EOF
    }
  }

  systemd {
    service_unit "dbf-systemd-extensions-worker" {
      description = "DebianForm systemd extension timer worker v2"
      type        = "oneshot"

      run = [
        "/usr/local/bin/dbf-systemd-extensions-worker",
      ]

      stdout    = "journal"
      stderr    = "journal"
      wanted_by = []

      service_config = {
        NoNewPrivileges = true
        PrivateTmp      = true
      }
    }

    timer "dbf-systemd-extensions-worker" {
      description = "DebianForm systemd extension timer v2"
      enable      = true
      state       = "running"

      timer = {
        Unit            = "dbf-systemd-extensions-worker.service"
        OnActiveSec     = "1s"
        OnBootSec       = "1s"
        OnUnitActiveSec = "3s"
        AccuracySec     = "2s"
        Persistent      = true
      }
    }

    resolved {
      enable = true
      state  = "running"

      resolve = {
        DNS             = ["9.9.9.9"]
        DNSSEC          = "allow-downgrade"
        DNSStubListener = false
      }
    }

    journald {
      journal = {
        Compress     = true
        SystemMaxUse = "128M"
      }
    }
  }
}
