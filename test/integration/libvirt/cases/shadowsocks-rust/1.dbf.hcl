component "shadowsocks_rust" {
  type    = "binary"
  version = "1.24.0"

  source "amd64" {
    url    = "https://github.com/shadowsocks/shadowsocks-rust/releases/download/v1.24.0/shadowsocks-v1.24.0.x86_64-unknown-linux-gnu.tar.xz"
    sha256 = "5f528efb4e51e732352f5c69538dcc76e8cf8f6d1a240dfb5b748a67f0b05f65"
  }

  extract {
    include = "ssserver"
  }

  install {
    path  = "/usr/local/bin/ssserver"
    owner = "root"
    group = "root"
    mode  = "0755"
  }

  groups {
    group "shadowsocks" {
      system = true
    }
  }

  users {
    user "shadowsocks" {
      group  = "shadowsocks"
      home   = "/var/lib/shadowsocks-rust"
      shell  = "/usr/sbin/nologin"
      system = true
    }
  }

  directories {
    directory "/etc/shadowsocks-rust" {
      owner = "root"
      group = "shadowsocks"
      mode  = "0750"
    }

    directory "/var/lib/shadowsocks-rust" {
      owner = "shadowsocks"
      group = "shadowsocks"
      mode  = "0750"
    }
  }

  files {
    file "/etc/shadowsocks-rust/server.json" {
      owner = "root"
      group = "shadowsocks"
      mode  = "0640"

      content = <<-EOF
        {
          "server": "0.0.0.0",
          "server_port": 18388,
          "password": "debianform-integration-password",
          "method": "chacha20-ietf-poly1305",
          "mode": "tcp_and_udp"
        }
      EOF
    }
  }

  systemd {
    service_unit "shadowsocks-rust" {
      description = "Shadowsocks Rust Server"

      run = [
        "/usr/local/bin/ssserver",
        "-c",
        "/etc/shadowsocks-rust/server.json",
      ]

      user          = "shadowsocks"
      group         = "shadowsocks"
      working_dir   = "/var/lib/shadowsocks-rust"
      restart       = "always"
      restart_delay = "2s"
      after         = ["network-online.target"]
      wants         = ["network-online.target"]
      stdout        = "journal"
      stderr        = "journal"
    }
  }

  services {
    service "shadowsocks-rust" {
      enabled = true
      state   = "running"
    }
  }
}

host "cihost" {
  ssh {
    host          = "__DBF_VM_IP__"
    user          = "root"
    identity_file = "${path.module}/id_ed25519"
  }

  state {
    path      = "/var/lib/debianform-integration/shadowsocks-rust-state.json"
    lock_path = "/var/lock/debianform-integration/shadowsocks-rust-state.lock"
  }

  system {
    architecture = "amd64"
    codename     = "trixie"
  }

  components = [
    component.shadowsocks_rust,
  ]
}
