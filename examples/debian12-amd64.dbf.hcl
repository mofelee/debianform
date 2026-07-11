host "debian12_amd64" {
  platform {
    architecture = "amd64"
    codename     = "bookworm"
  }

  assert {
    condition = self.platform.architecture == "amd64" && self.platform.codename == "bookworm"
    message   = "This smoke example requires Debian 12 bookworm on amd64."
  }
}
