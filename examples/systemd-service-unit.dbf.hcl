# DebianForm systemd service_unit 示例。
#
# 该示例同时展示两种服务 unit 写法：
# - systemd.unit: 纯文本 systemd unit，适合需要完整控制 unit 内容的场景。
# - systemd.service_unit: 结构化写法，适合常见长驻服务。
# 两者最终都会展开为 /etc/systemd/system/*.service，并由 services.service
# 管理 enabled/running 状态。

host "service_unit1" {
  directories {
    directory "/var/lib/myapp" {
      owner = "root"
      group = "root"
      mode  = "0755"
    }
  }

  files {
    file "/etc/myapp/config.yaml" {
      owner = "root"
      group = "root"
      mode  = "0644"

      content = <<-EOF
        listen: 127.0.0.1:8080
        data_dir: /var/lib/myapp
      EOF
    }

    file "/usr/local/bin/myapp-worker" {
      owner = "root"
      group = "root"
      mode  = "0755"

      content = <<-EOF
        #!/usr/bin/env sh
        exec sleep infinity
      EOF
    }
  }

  systemd {
    unit "myapp-raw.service" {
      content = <<-EOF
        [Unit]
        Description=My App Raw Unit
        Wants=network-online.target
        After=network-online.target

        [Service]
        WorkingDirectory=/var/lib/myapp
        Environment=MYAPP_MODE=raw
        ExecStart=/usr/local/bin/myapp-worker --config /etc/myapp/config.yaml
        Restart=always
        RestartSec=5s
        StandardOutput=journal
        StandardError=journal

        [Install]
        WantedBy=multi-user.target
      EOF
    }

    service_unit "myapp-structured" {
      description = "My App Structured Unit"

      run = [
        "/usr/local/bin/myapp-worker",
        "--config",
        "/etc/myapp/config.yaml",
      ]

      working_dir   = "/var/lib/myapp"
      restart       = "always"
      restart_delay = "5s"

      wants = ["network-online.target"]
      after = ["network-online.target"]

      environment = {
        MYAPP_MODE = "structured"
      }

      stdout = "journal"
      stderr = "journal"
    }
  }

  services {
    service "myapp-raw" {
      enabled = true
      state   = "running"
    }

    service "myapp-structured" {
      enabled = true
      state   = "running"
    }
  }
}
