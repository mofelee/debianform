# 部署 shadowsocks-rust v1.24.0 的服务端。
#
# 这个示例使用官方 musl 静态构建，支持 Debian amd64 和 arm64。发布归档与
# ssservice 二进制都使用 SHA-256 校验；目标主机只在缺少下载/解压工具时安装
# ca-certificates、curl、tar 和 xz-utils。
#
# 使用前必须修改 local.password，并按需修改端口和主机。
# 这个配置不会修改防火墙或云安全组；tcp_and_udp 模式需要同时放行对应的 TCP/UDP
# 端口。

locals {
  host         = "server1"
  version      = "1.24.0"
  service_name = "shadowsocks-rust-server.service"
  server_port  = 8388
  password     = "CHANGE-ME-TO-A-LONG-RANDOM-PASSWORD"
}

state "ssh" {
  host      = local.host
  path      = "/var/lib/debianform/shadowsocks-rust-state.json"
  lock_path = "/var/lock/debianform/shadowsocks-rust-state.lock"
}

handler "restart_shadowsocks_rust" {
  host    = local.host
  command = "systemctl restart shadowsocks-rust-server.service"
}

debian_directory "shadowsocks_rust_config" {
  host = local.host
  path = "/etc/shadowsocks-rust"
  mode = "0755"
}

debian_release_binary "ssservice" {
  host           = local.host
  path           = "/usr/local/bin/ssservice"
  member         = "ssservice"
  archive_format = "tar.xz"

  sources = {
    amd64 = {
      url            = "https://github.com/shadowsocks/shadowsocks-rust/releases/download/v${local.version}/shadowsocks-v${local.version}.x86_64-unknown-linux-musl.tar.xz"
      archive_sha256 = "0d84f5f350ec99396867d718f146fc3810975b2a7cd06192f158d96bdef460e7"
      binary_sha256  = "a4c31c69f383eeba3969e272a368d88bb71b165e82c74dea1c186da161b18a85"
    }
    arm64 = {
      url            = "https://github.com/shadowsocks/shadowsocks-rust/releases/download/v${local.version}/shadowsocks-v${local.version}.aarch64-unknown-linux-musl.tar.xz"
      archive_sha256 = "e00b6551f40bb2d61adb2503909e0df6550c022372c812f3f34350510797ef2f"
      binary_sha256  = "c33f8c666f23166c79ed204bbb2ea35c890b74d07160c500b21879a8d9569a75"
    }
  }

  notify = [
    handler.restart_shadowsocks_rust,
  ]
}

# 配置文件不直接保存密码，只引用 systemd 注入的环境变量。
debian_file "shadowsocks_rust_server_config" {
  host = local.host
  path = "/etc/shadowsocks-rust/server.json"
  mode = "0644"

  content = <<-EOF
    {
      "server": "::",
      "server_port": ${local.server_port},
      "password": "$${SHADOWSOCKS_PASSWORD}",
      "method": "chacha20-ietf-poly1305",
      "mode": "tcp_and_udp",
      "timeout": 300,
      "nofile": 32768
    }
  EOF

  notify = [
    handler.restart_shadowsocks_rust,
  ]
}

debian_file "shadowsocks_rust_environment" {
  host    = local.host
  path    = "/etc/shadowsocks-rust/server.env"
  content = "SHADOWSOCKS_PASSWORD=${local.password}\n"
  mode    = "0600"

  notify = [
    handler.restart_shadowsocks_rust,
  ]
}

debian_systemd_unit "shadowsocks_rust_server" {
  host = local.host
  name = local.service_name

  content = <<-EOF
    [Unit]
    Description=Shadowsocks-rust Server
    Documentation=https://github.com/shadowsocks/shadowsocks-rust
    After=network-online.target
    Wants=network-online.target

    [Service]
    Type=simple
    DynamicUser=yes
    EnvironmentFile=/etc/shadowsocks-rust/server.env
    ExecStart=/usr/local/bin/ssservice server --log-without-time -c /etc/shadowsocks-rust/server.json
    Restart=on-failure
    RestartSec=5s
    LimitNOFILE=32768
    CapabilityBoundingSet=CAP_NET_BIND_SERVICE
    AmbientCapabilities=CAP_NET_BIND_SERVICE
    NoNewPrivileges=yes
    PrivateTmp=yes
    ProtectHome=yes
    ProtectSystem=strict

    [Install]
    WantedBy=multi-user.target
  EOF

  depends_on = [
    debian_release_binary.ssservice,
    debian_file.shadowsocks_rust_server_config,
    debian_file.shadowsocks_rust_environment,
  ]

  notify = [
    handler.restart_shadowsocks_rust,
  ]
}

# 同名 service 会自动依赖 debian_systemd_unit，不需要再写 depends_on。
debian_service "shadowsocks_rust_server" {
  host    = local.host
  name    = local.service_name
  enabled = true
  state   = "running"
}
