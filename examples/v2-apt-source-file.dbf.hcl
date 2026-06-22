# DebianForm v2 APT main source replacement example.
#
# This manages the main APT source file as plain text. The content can use the
# traditional one-line "deb ..." syntax, as shown here, or Debian deb822
# ".sources" syntax when path points at a .sources file.
#
# on_destroy options:
# - keep: remove DebianForm state only; leave the remote source file as-is.
# - restore: restore the file content captured before DebianForm first managed it.

host "apt-source1" {
  apt {
    source_file "main" {
      path = "/etc/apt/sources.list"

      content = <<-EOF
        deb https://mirrors.aliyun.com/debian/ trixie main contrib non-free non-free-firmware
        deb https://mirrors.aliyun.com/debian/ trixie-updates main contrib non-free non-free-firmware
        deb https://mirrors.aliyun.com/debian-security/ trixie-security main contrib non-free non-free-firmware
      EOF

      on_destroy = "restore"
    }
  }
}

# deb822 variant example:
#
# path = "/etc/apt/sources.list.d/debian.sources"
# content = <<-EOF
#   Types: deb
#   URIs: https://mirrors.aliyun.com/debian/
#   Suites: trixie trixie-updates
#   Components: main contrib non-free non-free-firmware
#
#   Types: deb
#   URIs: https://mirrors.aliyun.com/debian-security/
#   Suites: trixie-security
#   Components: main contrib non-free non-free-firmware
# EOF
