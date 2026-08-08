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
    package "apache2" {
      conffile_policy = "keep"
    }
  }

  files {
    file "/etc/apache2/ports.conf" {
      depends_on = [package.apache2]
      owner      = "root"
      group      = "root"
      mode       = "0644"

      content = <<-EOF
        # Managed by DebianForm's resource-dependencies integration case.
        Listen 8080
      EOF
    }
  }

  services {
    service "apache2" {
      package    = "apache2"
      depends_on = [file["/etc/apache2/ports.conf"]]
      enabled    = true
      state      = "running"
    }
  }
}
