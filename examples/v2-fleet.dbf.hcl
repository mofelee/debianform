# DebianForm v2 设计示例。
#
# 本文件用于冻结 v2 DSL 的目标形态；当前 v1 执行器还不能 apply 此语法。
# 示例刻意同时展示两种复用边界：
#
# - profile：复用并合并主机基线配置。
# - component：复用一个部署单元，并在挂载它的 host 上展开资源图。
#
# `REPLACE_*`、示例域名和公钥都是待替换占位符；正式 validate 应拒绝它们。
# secrets/ 下的文件只表示本地 secret 输入，不能提交到版本库。DebianForm 的
# plan 和 state 只能记录其摘要，不能记录明文内容。

locals {
  administrator_key = "ssh-ed25519 AAAA_REPLACE_ME"
}

profile "base" {
  system {
    timezone = "Asia/Tokyo"
    locale   = "en_US.UTF-8"
  }

  packages {
    install = [
      "ca-certificates",
      "curl",
      "htop",
      "locales",
      "nftables",
      "openssh-server",
      "sudo",
      "tzdata",
      "vim",
    ]
  }

  environment {
    variables = {
      EDITOR = "vim"
      LANG   = "en_US.UTF-8"
    }

    shell_aliases = {
      la = "ls -A"
      ll = "ls -alF"
    }

    profile_scripts = {
      company_path = <<-EOF
        export PATH="/opt/company/bin:$PATH"
      EOF
    }
  }

  groups {
    group "deploy" {}
  }

  users {
    user "deploy" {
      home   = "/home/deploy"
      shell  = "/bin/bash"
      group  = "deploy"
      groups = ["sudo"]

      ssh_authorized_keys = [
        local.administrator_key,
      ]
    }
  }

  sudo {
    enable              = true
    passwordless_groups = ["sudo"]
  }

  sshd {
    enable = true
    port   = 22

    settings = {
      PasswordAuthentication = "no"
      PermitRootLogin        = "no"
      PubkeyAuthentication   = "yes"
    }
  }
}

profile "bbr" {
  kernel {
    modules = ["tcp_bbr"]

    sysctl = {
      "net.core.default_qdisc"          = "fq"
      "net.ipv4.tcp_congestion_control" = "bbr"
    }
  }
}

profile "wireguard" {
  packages {
    install = ["wireguard-tools"]
  }

  kernel {
    modules = ["wireguard"]
  }

  directories {
    directory "/etc/wireguard" {
      owner = "root"
      group = "systemd-network"
      mode  = "0750"
    }
  }
}

component "rclone" {
  type    = "binary"
  version = "1.66.0"

  source "amd64" {
    url    = "https://downloads.rclone.org/v1.66.0/rclone-v1.66.0-linux-amd64.zip"
    sha256 = "REPLACE_WITH_RCLONE_AMD64_SHA256"
  }

  source "arm64" {
    url    = "https://downloads.rclone.org/v1.66.0/rclone-v1.66.0-linux-arm64.zip"
    sha256 = "REPLACE_WITH_RCLONE_ARM64_SHA256"
  }

  extract {
    format           = "zip"
    strip_components = 1
    include          = "rclone"
  }

  install {
    path  = "/usr/local/bin/rclone"
    owner = "root"
    group = "root"
    mode  = "0755"
  }
}

component "company_ca" {
  type    = "ca_certificate"
  version = "2026.06.20"

  source {
    url    = "https://example.com/company-ca.crt"
    sha256 = "REPLACE_WITH_COMPANY_CA_SHA256"
  }

  install {
    path  = "/usr/local/share/ca-certificates/company-ca.crt"
    owner = "root"
    group = "root"
    mode  = "0644"
  }

  # ca_certificate 是有语义的资源类型。证书变化时由编译器生成
  # update-ca-certificates 激活动作，不需要用户写 after_install shell。
}

component "myapp" {
  type    = "archive"
  version = "2026.06.20"

  source "amd64" {
    url    = "https://example.com/releases/myapp-2026.06.20-linux-amd64.tar.gz"
    sha256 = "REPLACE_WITH_MYAPP_AMD64_SHA256"
  }

  source "arm64" {
    url    = "https://example.com/releases/myapp-2026.06.20-linux-arm64.tar.gz"
    sha256 = "REPLACE_WITH_MYAPP_ARM64_SHA256"
  }

  extract {
    format           = "tar.gz"
    strip_components = 1
  }

  install {
    path  = "/opt/myapp"
    owner = "myapp"
    group = "myapp"
    mode  = "0755"
  }

  groups {
    group "myapp" {
      system = true
    }
  }

  users {
    user "myapp" {
      system = true
      home   = "/var/lib/myapp"
      shell  = "/usr/sbin/nologin"
      group  = "myapp"
    }
  }

  directories {
    directory "/etc/myapp" {
      owner = "root"
      group = "root"
      mode  = "0755"
    }

    directory "/var/lib/myapp" {
      owner = "myapp"
      group = "myapp"
      mode  = "0750"
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
  }

  systemd {
    service "myapp" {
      enable      = true
      state       = "running"
      description = "My App"

      wanted_by = ["multi-user.target"]
      wants     = ["network-online.target"]
      after     = ["network-online.target"]

      environment = {
        MYAPP_ENV = "production"
      }

      service_config = {
        ExecStart        = "/opt/myapp/bin/myapp --config /etc/myapp/config.yaml"
        Group            = "myapp"
        NoNewPrivileges  = true
        PrivateTmp       = true
        ProtectHome      = true
        ProtectSystem    = "strict"
        ReadWritePaths   = ["/var/lib/myapp"]
        Restart          = "on-failure"
        RestartSec       = "5s"
        User             = "myapp"
        WorkingDirectory = "/var/lib/myapp"
      }
    }
  }
}

component "restic" {
  type    = "binary"
  version = "0.16.5"

  source "amd64" {
    url    = "https://github.com/restic/restic/releases/download/v0.16.5/restic_0.16.5_linux_amd64.bz2"
    sha256 = "REPLACE_WITH_RESTIC_AMD64_SHA256"
  }

  source "arm64" {
    url    = "https://github.com/restic/restic/releases/download/v0.16.5/restic_0.16.5_linux_arm64.bz2"
    sha256 = "REPLACE_WITH_RESTIC_ARM64_SHA256"
  }

  extract {
    format = "bz2"
  }

  install {
    path  = "/usr/local/bin/restic"
    owner = "root"
    group = "root"
    mode  = "0755"
  }

  directories {
    directory "/etc/restic" {
      owner = "root"
      group = "root"
      mode  = "0700"
    }
  }

  files {
    file "/etc/restic/environment" {
      owner     = "root"
      group     = "root"
      mode      = "0600"
      sensitive = true
      content   = file("secrets/server1-restic-environment")
    }

    file "/usr/local/bin/restic-backup" {
      owner = "root"
      group = "root"
      mode  = "0755"

      content = <<-EOF
        #!/usr/bin/env bash
        set -euo pipefail

        exec /usr/local/bin/restic backup /etc /opt /var/lib
      EOF
    }
  }

  systemd {
    service "restic-backup" {
      description = "Restic backup job"

      service_config = {
        EnvironmentFile = "/etc/restic/environment"
        ExecStart       = "/usr/local/bin/restic-backup"
        Type            = "oneshot"
      }
    }

    timer "restic-backup" {
      enable      = true
      state       = "running"
      description = "Run Restic backup daily"

      wanted_by = ["timers.target"]

      timer_config = {
        OnCalendar         = "daily"
        Persistent         = true
        RandomizedDelaySec = "30m"
      }
    }
  }
}

host "server1" {
  imports = [
    profile.base,
    profile.bbr,
    profile.wireguard,
  ]

  components = [
    component.rclone,
    component.company_ca,
    component.myapp,
    component.restic,
  ]

  ssh {
    host = "server1"
  }

  state {
    path      = "/var/lib/debianform/state/server1.yaml"
    lock_path = "/var/lock/debianform/state/server1.lock"
  }

  system {
    hostname     = "server1"
    architecture = "amd64"
    codename     = "trixie"
  }

  packages {
    install = [
      "docker-compose",
      "docker.io",
      "git",
      "systemd-resolved",
    ]
  }

  # profile.base 中 deploy.groups = ["sudo"]；host 的列表默认追加并去重，
  # 因此最终组列表是 ["sudo", "docker"]。
  users {
    user "deploy" {
      groups = ["docker"]
    }
  }

  groups {
    group "docker" {}
  }

  kernel {
    sysctl = {
      # 只有承担转发职责的主机才启用。
      "net.ipv4.ip_forward" = "1"
    }
  }

  files {
    file "/etc/wireguard/private.key" {
      owner     = "root"
      group     = "systemd-network"
      mode      = "0640"
      sensitive = true
      content   = file("secrets/server1-wireguard.key")
    }

    file "/opt/app/docker-compose.yml" {
      owner = "deploy"
      group = "deploy"
      mode  = "0644"

      content = <<-EOF
        services:
          web:
            image: nginx:1.27-alpine
            restart: unless-stopped
            ports:
              - "8080:80"
      EOF
    }
  }

  directories {
    directory "/opt/app" {
      owner = "deploy"
      group = "deploy"
      mode  = "0755"
    }
  }

  systemd {
    resolved {
      enable = true

      resolve = {
        DNS = [
          "1.1.1.1",
          "8.8.8.8",
        ]

        DNSStubListener = "yes"
      }
    }

    journald {
      journal = {
        MaxRetentionSec = "7day"
        Storage         = "persistent"
        SystemMaxUse    = "1G"
      }
    }

    networkd {
      enable = true

      network "10-eth0" {
        match = {
          Name = "eth0"
        }

        network = {
          DHCP = "yes"
        }

        dhcp_v4 = {
          UseDNS = false
        }
      }

      netdev "20-wg0" {
        netdev = {
          Kind = "wireguard"
          Name = "wg0"
        }

        wireguard = {
          ListenPort     = 51820
          PrivateKeyFile = "/etc/wireguard/private.key"

          # 不根据 peer.AllowedIPs 自动向系统路由表写路由。
          RouteTable = "off"
        }

        wireguard_peer "server2" {
          PublicKey = "SERVER2_PUBLIC_KEY"

          AllowedIPs = [
            "10.100.0.2/32",
            "10.200.0.0/24",
          ]

          Endpoint            = "server2.example.com:51820"
          PersistentKeepalive = 25
        }

        wireguard_peer "laptop" {
          PublicKey = "LAPTOP_PUBLIC_KEY"

          AllowedIPs = [
            "10.100.0.10/32",
          ]

          PersistentKeepalive = 25
        }
      }

      network "20-wg0" {
        match = {
          Name = "wg0"
        }

        network = {
          Address      = ["10.100.0.1/24"]
          IPv6AcceptRA = "no"
        }

        link = {
          RequiredForOnline = "no"
        }
      }

      link "10-eth0" {
        match = {
          OriginalName = "eth0"
        }

        link = {
          MTUBytes = 1500
        }
      }
    }

    service "cleanup-tmp" {
      description = "Clean old temporary files"

      service_config = {
        ExecStart = "/usr/bin/find /tmp -xdev -type f -mtime +7 -delete"
        Type      = "oneshot"
      }
    }

    timer "cleanup-tmp" {
      enable = true
      state  = "running"

      wanted_by = ["timers.target"]

      timer_config = {
        OnCalendar = "daily"
        Persistent = true
      }
    }
  }

  nftables {
    enable = true

    main {
      path     = "/etc/nftables.conf"
      validate = true
      activate = true

      content = <<-EOF
        flush ruleset

        include "/etc/nftables.d/*.nft"
      EOF
    }

    file "20-server1-filter" {
      path = "/etc/nftables.d/20-server1-filter.nft"

      content = <<-EOF
        table inet filter {
          chain input {
            type filter hook input priority 0; policy drop;

            ct state established,related accept
            iifname "lo" accept

            tcp dport { 22, 8080 } accept
            udp dport 51820 accept

            counter drop
          }

          chain forward {
            type filter hook forward priority 0; policy drop;
          }

          chain output {
            type filter hook output priority 0; policy accept;
          }
        }
      EOF
    }
  }

  docker {
    enable = true

    daemon_settings = {
      "log-driver" = "json-file"

      "log-opts" = {
        "max-file" = "3"
        "max-size" = "100m"
      }
    }

    compose "app" {
      enable    = true
      state     = "running"
      directory = "/opt/app"
      file      = "/opt/app/docker-compose.yml"

      wanted_by = ["multi-user.target"]
      after     = ["docker.service", "network-online.target"]
    }
  }

  assert {
    condition = contains(self.kernel.modules, "tcp_bbr")
    message   = "BBR requires the tcp_bbr kernel module."
  }

  assert {
    condition = self.kernel.sysctl["net.ipv4.tcp_congestion_control"] == "bbr"
    message   = "The BBR profile must set tcp_congestion_control to bbr."
  }

  assert {
    condition = self.systemd.networkd.netdev["20-wg0"].wireguard.RouteTable == "off"
    message   = "WireGuard must not auto-install AllowedIPs into a route table."
  }
}

host "server2" {
  imports = [
    profile.base,
    profile.bbr,
    profile.wireguard,
  ]

  components = [
    component.rclone,
    component.company_ca,
  ]

  ssh {
    host = "server2"
  }

  system {
    hostname     = "server2"
    architecture = "amd64"
    codename     = "trixie"
  }

  files {
    file "/etc/wireguard/private.key" {
      owner     = "root"
      group     = "systemd-network"
      mode      = "0640"
      sensitive = true
      content   = file("secrets/server2-wireguard.key")
    }
  }

  systemd {
    networkd {
      enable = true

      network "10-eth0" {
        match = {
          Name = "eth0"
        }

        network = {
          DHCP = "yes"
        }
      }

      netdev "20-wg0" {
        netdev = {
          Kind = "wireguard"
          Name = "wg0"
        }

        wireguard = {
          ListenPort     = 51820
          PrivateKeyFile = "/etc/wireguard/private.key"
          RouteTable     = "off"
        }

        wireguard_peer "server1" {
          PublicKey = "SERVER1_PUBLIC_KEY"

          AllowedIPs = [
            "10.10.0.0/24",
            "10.100.0.1/32",
          ]

          Endpoint            = "server1.example.com:51820"
          PersistentKeepalive = 25
        }
      }

      network "20-wg0" {
        match = {
          Name = "wg0"
        }

        network = {
          Address      = ["10.100.0.2/24"]
          IPv6AcceptRA = "no"
        }

        link = {
          RequiredForOnline = "no"
        }
      }
    }
  }

  nftables {
    enable = true

    main {
      path     = "/etc/nftables.conf"
      validate = true
      activate = true

      content = <<-EOF
        flush ruleset

        include "/etc/nftables.d/*.nft"
      EOF
    }

    file "20-server2-filter" {
      path = "/etc/nftables.d/20-server2-filter.nft"

      content = <<-EOF
        table inet filter {
          chain input {
            type filter hook input priority 0; policy drop;

            ct state established,related accept
            iifname "lo" accept

            tcp dport 22 accept
            udp dport 51820 accept

            counter drop
          }

          chain forward {
            type filter hook forward priority 0; policy drop;
          }

          chain output {
            type filter hook output priority 0; policy accept;
          }
        }
      EOF
    }
  }

  assert {
    condition = self.systemd.networkd.netdev["20-wg0"].wireguard.RouteTable == "off"
    message   = "WireGuard must not auto-install routes from AllowedIPs."
  }
}
