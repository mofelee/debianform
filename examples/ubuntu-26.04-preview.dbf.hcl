# Ubuntu 26.04 LTS amd64 Preview smoke example.
#
# This example intentionally manages no network resources. DebianForm does not
# generate, read the contents of, modify, or remove Netplan configuration.

host "ubuntu26_preview" {
  platform {
    distribution = "ubuntu"
    version      = "26.04"
    architecture = "amd64"
    codename     = "resolute"
  }

  files {
    file "/etc/debianform-ubuntu26-preview.txt" {
      owner = "root"
      group = "root"
      mode  = "0644"

      content = <<-EOF
        managed by DebianForm
        target=ubuntu-26.04-amd64
      EOF
    }
  }

  assert {
    condition = self.platform.distribution == "ubuntu" && self.platform.version == "26.04" && self.platform.architecture == "amd64" && self.platform.codename == "resolute"
    message   = "This Preview example requires Ubuntu 26.04 resolute on amd64."
  }
}
