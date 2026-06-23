host "docker-custom1" {
  docker {
    enable = true

    package {
      source = "custom"
    }

    compose "app" {
      directory = "/opt/app"

      file {
        path    = "/opt/app/compose.yaml"
        content = "services: {}\n"
      }
    }
  }
}
