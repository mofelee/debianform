host "docker-none1" {
  docker {
    enable = true

    package {
      source = "none"
    }

    daemon {
      settings = {
        "log-driver" = "json-file"
      }
    }
  }
}
