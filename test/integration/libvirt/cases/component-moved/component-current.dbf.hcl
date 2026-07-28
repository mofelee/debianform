component "bird_stack" {
  packages {
    package "openssh-server" {}
  }

  directories {
    directory "/etc/debianform-moved" {
      owner = "root"
      group = "root"
      mode  = "0755"
    }
  }

  script "reload_bird" {
    mode    = "once"
    outputs = [
      "/var/lib/debianform-moved/reload.count",
      "/var/lib/debianform-moved/component.name",
    ]

    content = <<-EOF
      set -eu
      install -d -m 0755 /var/lib/debianform-moved
      count_file=/var/lib/debianform-moved/reload.count
      count=0
      if [ -f "$count_file" ]; then
        count="$(cat "$count_file")"
      fi
      printf '%s\n' "$((count + 1))" > "$count_file"
      printf '%s\n' "$DBF_COMPONENT_NAME" > /var/lib/debianform-moved/component.name
    EOF
  }

  files {
    file "/etc/debianform-moved/bird.conf" {
      owner = "root"
      group = "root"
      mode  = "0644"

      content = "router id 192.0.2.1;\nprotocol ospf v3 edge { ipv6 { import all; export all; }; area 0 { interface \"eth0\"; }; }\n"

      on_change = script.reload_bird
    }

    file "/etc/debianform-moved/stable.conf" {
      owner   = "root"
      group   = "root"
      mode    = "0644"
      content = "log syslog all;\n"

      lifecycle {
        prevent_destroy = false
      }
    }
  }

  services {
    service "ssh" {
      package = "openssh-server"
      enabled = true
      state   = "running"
    }
  }
}
