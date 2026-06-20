host "debian_ci" {
  address       = "__DBF_VM_IP__"
  identity_file = "${path.module}/id_ed25519"
}

state "ssh" {
  host      = "debian_ci"
  path      = "/var/lib/debianform-integration/state.json"
  lock_path = "/var/lock/debianform-integration/state.lock"
}

handler "record_change" {
  host    = "debian_ci"
  command = "echo handler >> /run/debianform-files-handler.log"
}

locals {
  files = {
    primary   = "managed primary\n"
    secondary = "managed secondary\n"
  }
}

debian_directory "managed" {
  host  = "debian_ci"
  path  = "/var/lib/debianform-files"
  owner = "root"
  group = "root"
  mode  = "0750"
}

debian_file "managed" {
  for_each = local.files

  host    = "debian_ci"
  path    = each.key == "primary" ? "/var/lib/debianform-files/primary.conf" : "/var/lib/debianform-files/secondary.conf"
  content = each.value
  owner   = "root"
  group   = "root"
  mode    = each.key == "primary" ? "0640" : "0600"

  depends_on = [
    debian_directory.managed,
  ]

  notify = [
    handler.record_change,
  ]
}
