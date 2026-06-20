# DebianForm v2 user/group/authorized_key 示例。

host "users1" {
  ssh {
    host = "users1"
  }

  system {
    hostname     = "users1"
    architecture = "amd64"
    codename     = "trixie"
  }

  groups {
    group "deploy" {
      system = true
    }
  }

  users {
    user "deploy" {
      home  = "/home/deploy"
      shell = "/bin/bash"
      group = "deploy"

      ssh_authorized_keys = [
        "ssh-ed25519 AAAA_REPLACE_ME deploy@example",
      ]
    }
  }
}
