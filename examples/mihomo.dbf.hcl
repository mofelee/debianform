# DebianForm mihomo binary component example.
#
# This example installs the latest stable mihomo Linux binary release from
# GitHub, writes a minimal config, and manages mihomo with systemd. Replace the
# demo proxy/rules before using this for real traffic.

component "mihomo" {
  type    = "binary"
  version = "1.19.27"

  source "amd64" {
    url    = "https://github.com/MetaCubeX/mihomo/releases/download/v1.19.27/mihomo-linux-amd64-v1.19.27.gz"
    sha256 = "fb3e34c55844f389ff54679e5a3aec331d5ec38006c20f8dcc476fb47768a58f"
  }

  source "arm64" {
    url    = "https://github.com/MetaCubeX/mihomo/releases/download/v1.19.27/mihomo-linux-arm64-v1.19.27.gz"
    sha256 = "87db0c6660a9557a901b5750f997967e71d8c0af07ea1d1dd4d04c28da7f7e6f"
  }

  extract {
    format = "gz"
  }

  install {
    path  = "/usr/local/bin/mihomo"
    owner = "root"
    group = "root"
    mode  = "0755"
  }

  groups {
    group "mihomo" {
      system = true
    }
  }

  users {
    user "mihomo" {
      group  = "mihomo"
      home   = "/var/lib/mihomo"
      shell  = "/usr/sbin/nologin"
      system = true
    }
  }

  directories {
    directory "/etc/mihomo" {
      owner = "root"
      group = "mihomo"
      mode  = "0750"
    }

    directory "/var/lib/mihomo" {
      owner = "mihomo"
      group = "mihomo"
      mode  = "0750"
    }
  }

  files {
    file "/etc/mihomo/config.yaml" {
      owner = "root"
      group = "mihomo"
      mode  = "0640"

      content = <<-EOF
        port: 7890
        socks-port: 7891
        mixed-port: 7892
        allow-lan: false
        mode: rule
        log-level: info
        external-controller: 127.0.0.1:9090

        proxies:
          - name: example
            type: socks5
            server: 127.0.0.1
            port: 1080

        proxy-groups:
          - name: PROXY
            type: select
            proxies:
              - example
              - DIRECT

        rules:
          - MATCH,DIRECT
      EOF
    }
  }

  systemd {
    service_unit "mihomo" {
      description = "Mihomo Proxy"

      run = [
        "/usr/local/bin/mihomo",
        "-d",
        "/var/lib/mihomo",
        "-f",
        "/etc/mihomo/config.yaml",
      ]

      user          = "mihomo"
      group         = "mihomo"
      working_dir   = "/var/lib/mihomo"
      restart       = "always"
      restart_delay = "5s"
      after         = ["network-online.target"]
      wants         = ["network-online.target"]
      stdout        = "journal"
      stderr        = "journal"
    }
  }

  services {
    service "mihomo" {
      enabled = true
      state   = "running"
    }
  }
}

host "proxy1" {
  components = [
    component.mihomo,
  ]

  system {
    architecture = "amd64"
  }
}
