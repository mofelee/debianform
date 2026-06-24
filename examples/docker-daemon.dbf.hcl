# DebianForm Docker daemon 示例。
#
# daemon.settings 会映射到 /etc/docker/daemon.json，并在变化后 restart Docker。
# 离线 plan 需要显式声明目标 Debian runtime facts。

host "docker-daemon1" {
  system {
    architecture = "amd64"
    codename     = "trixie"
  }

  docker {
    enable = true

    daemon {
      settings = {
        "log-driver" = "json-file"
        "log-opts" = {
          "max-size" = "100m"
          "max-file" = "3"
        }
        "registry-mirrors" = [
          "https://mirror.example.com"
        ]
      }
    }
  }
}
