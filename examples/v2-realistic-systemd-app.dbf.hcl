# Realistic DebianForm v2 deployment template.
#
# This example deploys a tiny long-running service without external downloads or
# secrets. It is intended to stay runnable with:
#
#   dbf validate -f examples/v2-realistic-systemd-app.dbf.hcl
#   dbf plan -f examples/v2-realistic-systemd-app.dbf.hcl --offline

host "demoapp1" {
  groups {
    group "demoapp" {
      system = true
    }
  }

  users {
    user "demoapp" {
      system = true
      group  = "demoapp"
      home   = "/var/lib/demoapp"
      shell  = "/usr/sbin/nologin"
    }
  }

  directories {
    directory "/etc/demoapp" {
      owner = "root"
      group = "demoapp"
      mode  = "0750"
    }

    directory "/var/lib/demoapp" {
      owner = "demoapp"
      group = "demoapp"
      mode  = "0750"
    }
  }

  files {
    file "/etc/demoapp/config.env" {
      owner = "root"
      group = "demoapp"
      mode  = "0640"

      content = <<-EOF
        DEMOAPP_MESSAGE=hello-from-debianform
        DEMOAPP_INTERVAL=30
      EOF
    }

    file "/usr/local/bin/demoapp-worker" {
      owner = "root"
      group = "root"
      mode  = "0755"

      content = <<-EOF
        #!/usr/bin/env sh
        set -eu

        . /etc/demoapp/config.env

        while :; do
          printf '%s %s\n' "$(date -Is)" "$DEMOAPP_MESSAGE" >> /var/lib/demoapp/heartbeat.log
          sleep "$DEMOAPP_INTERVAL"
        done
      EOF
    }
  }

  systemd {
    service_unit "demoapp" {
      description = "DebianForm demo app"

      run = [
        "/usr/local/bin/demoapp-worker",
      ]

      user        = "demoapp"
      group       = "demoapp"
      working_dir = "/var/lib/demoapp"

      restart       = "always"
      restart_delay = "5s"

      wants = ["network-online.target"]
      after = ["network-online.target"]

      stdout = "journal"
      stderr = "journal"
    }
  }

  services {
    service "demoapp" {
      enabled = true
      state   = "running"
    }
  }
}
