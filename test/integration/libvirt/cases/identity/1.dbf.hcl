host "debian_ci" {
  address       = "__DBF_VM_IP__"
  identity_file = "${path.module}/id_ed25519"
}

state "ssh" {
  host      = "debian_ci"
  path      = "/var/lib/debianform-integration/state.json"
  lock_path = "/var/lock/debianform-integration/state.lock"
}

debian_group "deploy" {
  host = "debian_ci"
  name = "debianform-deploy"
  gid  = 4242
}

debian_user "app" {
  host  = "debian_ci"
  name  = "debianform-app"
  uid   = 4250
  gid   = "debianform-deploy"
  home  = "/home/debianform-app"
  shell = "/usr/sbin/nologin"

  depends_on = [
    debian_group.deploy,
  ]
}

debian_directory "app_home" {
  host  = "debian_ci"
  path  = "/home/debianform-app"
  owner = "debianform-app"
  group = "debianform-deploy"
  mode  = "0750"

  depends_on = [
    debian_user.app,
  ]
}

debian_authorized_key "app" {
  host   = "debian_ci"
  user   = "debianform-app"
  source = "${path.module}/app_key.pub"

  depends_on = [
    debian_directory.app_home,
  ]
}
