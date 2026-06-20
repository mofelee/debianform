# DebianForm v2 raw systemd unit + service 示例。

host "service1" {
  files {
    file "/etc/myapp/config.yaml" {
      owner   = "root"
      group   = "root"
      mode    = "0644"
      content = "listen: 127.0.0.1:8080\n"
    }
  }

  systemd {
    unit "myapp.service" {
      content = <<-EOF
        [Unit]
        Description=My App

        [Service]
        ExecStart=/usr/local/bin/myapp --config /etc/myapp/config.yaml
      EOF
    }
  }

  services {
    service "myapp" {
      enabled = true
      state   = "running"
    }
  }
}
