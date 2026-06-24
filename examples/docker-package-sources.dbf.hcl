host "docker-sources1" {
  system {
    architecture = "amd64"
    codename     = "trixie"
  }

  docker {
    enable = true

    package {
      source = "debian"
    }
  }
}
