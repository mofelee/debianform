state "ssh" {
  host      = "ksvm201"
  path      = "/tmp/debianform-fleet-smoke/state.json"
  lock_path = "/tmp/debianform-fleet-smoke/state.lock"
}

locals {
  ksvm_hosts = toset([
    "ksvm201",
    "ksvm202",
    "ksvm203",
    "ksvm204",
    "ksvm205",
    "ksvm206",
    "ksvm207",
    "ksvm209",
    "ksvm210",
    "ksvm211",
    "ksvm212",
    "ksvm213",
  ])
}

debian_directory "smoke_dir" {
  for_each = local.ksvm_hosts

  host = each.key
  path = "/tmp/debianform-fleet-smoke"
  mode = "0755"
}

debian_file "host_file" {
  for_each = local.ksvm_hosts

  host    = each.key
  path    = "/tmp/debianform-fleet-smoke/host.txt"
  content = "managed by debianform on ${each.key}\n"
  mode    = "0644"

  depends_on = [
    debian_directory.smoke_dir,
  ]
}
