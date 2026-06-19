host "debian_ci" {
  address       = "__DBF_VM_IP__"
  identity_file = "${path.module}/id_ed25519"
}

state "ssh" {
  host      = "debian_ci"
  path      = "/var/lib/debianform-integration/state.json"
  lock_path = "/var/lock/debianform-integration/state.lock"
}

debian_apt_source "example" {
  host       = "debian_ci"
  uris       = "https://example.invalid/debian"
  suites     = "trixie"
  components = "main"
  signed_by  = "/etc/apt/keyrings/example.gpg"
}
