host "cihost" {
  ssh {
    host          = "__DBF_VM_IP__"
    user          = "root"
    identity_file = "${path.module}/id_ed25519"
  }

  state {
    path      = "/var/lib/debianform-integration/resource-dependencies-state.json"
    lock_path = "/var/lock/debianform-integration/resource-dependencies-state.lock"
  }

  packages {
    package "cron" {
      conffile_policy = "keep"
    }
  }

  files {
    file "/etc/default/cron" {
      depends_on = [package.cron]
      owner      = "root"
      group      = "root"
      mode       = "0644"

      content = <<-EOF
        # Managed by DebianForm's resource-dependencies integration case.
        READ_ENV="yes"
      EOF
    }
  }

  services {
    service "cron" {
      package    = "cron"
      depends_on = [file["/etc/default/cron"]]
      enabled    = true
      state      = "running"
    }
  }
}
