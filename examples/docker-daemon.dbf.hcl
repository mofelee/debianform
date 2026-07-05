# DebianForm Docker daemon 示例。
#
# daemon.settings 会映射到 /etc/docker/daemon.json，并在变化后 restart Docker。
# 在线 plan 会自动发现目标 Debian platform facts。若要离线 plan，
# 请在 host 中显式声明 platform.architecture 和 platform.codename。

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
