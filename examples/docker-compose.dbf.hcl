# DebianForm Docker Compose 示例。
#
# Compose 的原生 YAML 仍然由 compose.yaml 表达；DebianForm 管理 project 文件、
# env 文件、配置校验、默认 systemd unit、daemon-reload、开机启动服务和 project 状态。
# 在线 plan 会自动发现目标 Debian platform facts。若要离线 plan，
# 请在 host 中显式声明 platform.architecture 和 platform.codename。

host "compose1" {
  docker {
    enable = true

    compose "app" {
      state     = "running"
      directory = "/opt/app"

      file {
        path = "/opt/app/compose.yaml"

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
        content = "TOKEN=not-a-real-preview-secret\n"
        mode    = "0600"
      }

      after     = ["docker.service", "network-online.target"]
      wanted_by = ["multi-user.target"]
    }
  }
}
