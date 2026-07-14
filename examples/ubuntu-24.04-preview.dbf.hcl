# Ubuntu 24.04 LTS amd64 Preview smoke example.
#
# This example intentionally manages no network resources. DebianForm does not
# generate, read the contents of, modify, or remove Netplan configuration.

host "ubuntu24_preview" {
  platform {
    distribution = "ubuntu"
    version      = "24.04"
    architecture = "amd64"
    codename     = "noble"
  }

  files {
    file "/etc/debianform-ubuntu-preview.txt" {
      owner = "root"
      group = "root"
      mode  = "0644"

      content = <<-EOF
        managed by DebianForm
        target=ubuntu-24.04-amd64
      EOF
    }
  }

  assert {
    condition = self.platform.distribution == "ubuntu" && self.platform.version == "24.04" && self.platform.architecture == "amd64" && self.platform.codename == "noble"
    message   = "This Preview example requires Ubuntu 24.04 noble on amd64."
  }
}
