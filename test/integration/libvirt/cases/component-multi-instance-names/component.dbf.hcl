component "transport" {
  input "tag" {
    type     = string
    nullable = false
  }

  directories {
    directory "state" {
      path  = "/var/lib/debianform-multi/${input.tag}"
      owner = "root"
      group = "root"
      mode  = "0755"
    }
  }

  files {
    file "conf" {
      path    = "/etc/debianform-multi-${input.tag}.conf"
      owner   = "root"
      group   = "root"
      mode    = "0644"
      content = "tag=${input.tag}\n"
    }
  }

  systemd {
    service_unit "worker" {
      name        = "dbf-transport-${input.tag}"
      description = "DebianForm multi-instance transport ${input.tag}"
      run         = ["/usr/bin/sleep", "infinity"]
      wanted_by   = ["multi-user.target"]
    }
  }

  services {
    service "worker" {
      name    = "dbf-transport-${input.tag}"
      enabled = true
      state   = "running"
    }
  }
}
