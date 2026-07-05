host "docker-sources1" {
  docker {
    enable = true

    package {
      source = "debian"
    }
  }
}
