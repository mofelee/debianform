# DebianForm v2 Docker daemon 示例。
#
# daemon.settings 会在后续 loop 中映射到 /etc/docker/daemon.json。

host "docker-daemon1" {
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
