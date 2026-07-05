# DebianForm fleet syntax cheatsheet.
#
# This file is intentionally broad but still runnable:
#
#   dbf validate -f examples/fleet.dbf.hcl
#   dbf plan -f examples/fleet.dbf.hcl --offline
#
# It is not a production deployment. Replace example hosts, keys, URLs, hashes,
# CIDRs and secret source paths before applying to a real machine.

locals {
  admin_key = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIexamplepreviewkey admin@example"
}

variable "environment" {
  type        = string
  description = "Deployment environment label rendered into config files."
  default     = "staging"
  nullable    = false

  validation {
    condition     = contains(["dev", "staging", "prod"], var.environment)
    error_message = "environment must be dev, staging, or prod."
  }
}

variable "api_token" {
  type        = string
  description = "Example sensitive runtime value."
  default     = "not-a-real-preview-token"
  sensitive   = true
  ephemeral   = true
}

variable "wireguard_private_key" {
  type        = string
  description = "Example WireGuard private key runtime value."
  default     = "not-a-real-wireguard-private-key"
  sensitive   = true
  ephemeral   = true
}

profile "debian_base" {
  system {
    timezone = "UTC"
    locale   = "en_US.UTF-8"
  }

  packages {
    install = [
      "ca-certificates",
      "curl",
      "htop",
      "nftables",
      "tzdata",
      "vim",
    ]
  }

  apt {
    source_file "local-note" {
      path    = "/etc/apt/sources.list.d/local-note.sources"
      content = "# managed by DebianForm\n"
      ensure  = "present"
    }
  }

  groups {
    group "deploy" {}
  }

  users {
    user "deploy" {
      group  = "deploy"
      groups = ["docker"]
      home   = "/home/deploy"
      shell  = "/bin/bash"

      ssh_authorized_keys = [
        local.admin_key,
      ]
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

  assert {
    condition = self.kernel.sysctl["net.ipv4.tcp_congestion_control"] == "bbr"
    message   = "BBR profile must keep tcp_bbr congestion control enabled."
  }
}

component "static_tool" {
  type    = "binary"
  version = "1.0.0"

  source "amd64" {
    url    = "https://downloads.example.invalid/static-tool-1.0.0-linux-amd64.tar.gz"
    sha256 = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
  }

  source "arm64" {
    url    = "https://downloads.example.invalid/static-tool-1.0.0-linux-arm64.tar.gz"
    sha256 = "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
  }

  extract {
    format           = "tar.gz"
    strip_components = 1
    include          = "static-tool"
  }

  install {
    path = "/usr/local/bin/static-tool"
    mode = "0755"
  }
}

component "webapp" {
  input "listen_addr" {
    type        = string
    description = "Address passed to the generated systemd service."
    default     = "127.0.0.1:8080"
    nullable    = false
  }

  input "extra_environment" {
    type        = map(string)
    description = "Additional app environment values."
    default     = {}
    nullable    = false
  }

  groups {
    group "webapp" {
      system = true
    }
  }

  users {
    user "webapp" {
      system = true
      group  = "webapp"
      home   = "/var/lib/webapp"
      shell  = "/usr/sbin/nologin"
    }
  }

  directories {
    directory "/etc/webapp" {}

    directory "/var/lib/webapp" {
      owner = "webapp"
      group = "webapp"
      mode  = "0750"
    }
  }

  files {
    file "/etc/webapp/config.env" {
      owner = "root"
      group = "webapp"
      mode  = "0640"

      content = <<-ENV
        WEBAPP_ENV=${var.environment}
        LISTEN_ADDR=${input.listen_addr}
      ENV
    }

    file "/etc/webapp/extra-env.json" {
      owner           = "root"
      group           = "webapp"
      mode            = "0640"
      sensitive       = true
      content         = jsonencode(input.extra_environment)
      content_version = "2026-06-24-example"
    }
  }

  systemd {
    service_unit "webapp" {
      description = "Example Web App"
      run         = ["/usr/local/bin/static-tool", "serve", "--listen", input.listen_addr]
      type        = "simple"
      user        = "webapp"
      group       = "webapp"
      working_dir = "/var/lib/webapp"
      restart     = "always"
      after       = ["network-online.target"]
      wants       = ["network-online.target"]

      environment = {
        WEBAPP_ENV = var.environment
      }

      service_config = {
        AmbientCapabilities = "CAP_NET_BIND_SERVICE"
        EnvironmentFile     = "/etc/webapp/config.env"
        NoNewPrivileges     = true
        PrivateTmp          = true
        ProtectHome         = true
        ProtectSystem       = "strict"
        ReadWritePaths      = ["/var/lib/webapp"]
      }
    }
  }

  services {
    service "webapp" {
      enabled = true
      state   = "running"
    }
  }
}

host "fleet1" {
  imports = [
    profile.debian_base,
    profile.bbr,
  ]

  components = [
    component.static_tool,
  ]

  component "webapp" {
    source = component.webapp

    inputs = {
      listen_addr = "127.0.0.1:8080"
      extra_environment = {
        API_TOKEN = var.api_token
      }
    }
  }

  ssh {
    host          = "fleet1.example.invalid"
    port          = 22
    user          = "root"
    identity_file = "~/.ssh/id_ed25519"
  }

  state {
    path      = "/var/lib/debianform/state/fleet1.json"
    lock_path = "/var/lock/debianform/state/fleet1.lock"
  }

  system {
    hostname = "fleet1"
  }

  platform {
    architecture = "amd64"
    codename     = "trixie"
  }

  apt {
    repository "example_tools" {
      uris          = ["https://repo.example.invalid/debian"]
      suites        = ["trixie"]
      components    = ["main"]
      architectures = ["amd64"]

      signing_key {
        url    = "https://repo.example.invalid/key.asc"
        sha256 = "1111111111111111111111111111111111111111111111111111111111111111"
        path   = "/etc/apt/keyrings/example-tools.asc"
      }
    }
  }

  packages {
    package "example-tool" {
      repositories = ["example_tools"]
    }

    install = [
      "git",
      "systemd-resolved",
    ]
  }

  groups {
    group "docker" {}
  }

  directories {
    directory "/opt/app" {
      owner = "deploy"
      group = "deploy"
      mode  = "0755"
    }

    directory "/etc/nftables.d" {}
  }

  files {
    file "/etc/example-token" {
      mode            = "0600"
      sensitive       = true
      content         = var.api_token
      content_version = "2026-06-24-example"
    }

    file "/etc/wireguard/wg0.key" {
      owner           = "root"
      group           = "systemd-network"
      mode            = "0640"
      sensitive       = true
      content         = var.wireguard_private_key
      content_version = "2026-06-24-example"
    }
  }

  systemd {
    resolved {
      enable = true
      state  = "running"

      resolve = {
        DNS             = ["1.1.1.1", "9.9.9.9"]
        DNSSEC          = "allow-downgrade"
        DNSStubListener = true
      }
    }

    journald {
      journal = {
        Compress        = true
        MaxRetentionSec = "7day"
        Storage         = "persistent"
        SystemMaxUse    = "1G"
      }
    }

    networkd {
      enable = true

      netdev "wg0" {
        netdev = {
          Name = "wg0"
          Kind = "wireguard"
        }

        wireguard = {
          ListenPort     = 51820
          PrivateKeyFile = "/etc/wireguard/wg0.key"
          RouteTable     = "off"
        }

        wireguard_peer "peer1" {
          PublicKey           = "peer1-public-key-placeholder"
          AllowedIPs          = ["10.80.0.2/32"]
          Endpoint            = "peer1.example.invalid:51820"
          PersistentKeepalive = 25
        }
      }

      network "wg0" {
        match = {
          Name = "wg0"
        }

        network = {
          Address = ["10.80.0.1/30"]
        }
      }
    }

    unit "oneshot-maintenance.service" {
      content = <<-UNIT
        [Unit]
        Description=Example raw oneshot unit

        [Service]
        Type=oneshot
        ExecStart=/usr/bin/true
      UNIT
    }

    service_unit "cleanup-cache" {
      description = "Clean cache"
      run         = ["/usr/bin/find", "/var/cache", "-type", "f", "-mtime", "+30", "-delete"]
      type        = "oneshot"
      wanted_by   = []
    }

    timer "cleanup-cache" {
      description = "Run cache cleanup daily"
      enable      = true
      state       = "running"

      timer = {
        Unit               = "cleanup-cache.service"
        OnCalendar         = "daily"
        Persistent         = true
        RandomizedDelaySec = "30m"
      }
    }
  }

  services {
    service "oneshot-maintenance" {
      state = "stopped"
    }
  }

  nftables {
    enable = true

    main {
      path     = "/etc/nftables.conf"
      validate = true
      activate = true

      content = <<-NFT
        flush ruleset

        include "/etc/nftables.d/*.nft"
      NFT
    }

    file "20-input" {
      path = "/etc/nftables.d/20-input.nft"

      content = <<-NFT
        table inet filter {
          chain input {
            type filter hook input priority 0; policy drop;

            ct state established,related accept
            iifname "lo" accept
            tcp dport { 22, 8080 } accept
            udp dport 51820 accept

            counter drop
          }
        }
      NFT
    }
  }

  docker {
    enable = true
    users  = ["deploy"]

    package {
      source           = "debian"
      remove_conflicts = "auto"
    }

    daemon {
      settings = {
        "log-driver" = "json-file"
        "log-opts" = {
          "max-file" = "3"
          "max-size" = "100m"
        }
      }
    }

    compose "app" {
      state     = "running"
      directory = "/opt/app"

      file {
        path  = "/opt/app/compose.yaml"
        owner = "deploy"
        group = "deploy"
        mode  = "0644"

        content = <<-YAML
          services:
            web:
              image: nginx:1.27-alpine
              ports:
                - "8080:80"
        YAML
      }

      env_file "app" {
        path    = "/opt/app/.env"
        mode    = "0600"
        content = "APP_ENV=${var.environment}\n"
      }
    }
  }

  assert {
    condition = self.systemd.resolved.resolve.DNS[0] == "1.1.1.1"
    message   = "fleet1 should use the documented resolver example."
  }
}
