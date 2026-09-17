component "transport" {
  input "tag" {
    type     = string
    nullable = false
  }

  files {
    file "conf" {
      path       = "/etc/demo-${input.tag}/app.conf"
      owner      = "root"
      group      = "root"
      mode       = "0600"
      content    = "tag=${input.tag}\n"
      depends_on = [service.svc]
    }
  }

  directories {
    directory "state" {
      path  = "/var/lib/demo-${input.tag}"
      owner = "root"
      group = "root"
      mode  = "0750"
    }
  }

  systemd {
    unit "raw" {
      name    = "demo-raw-${input.tag}.service"
      content = "[Unit]\nDescription=raw ${input.tag}\n"
    }

    service_unit "unit" {
      name = "demo-${input.tag}"
      run  = ["/usr/bin/true"]
    }
  }

  services {
    service "svc" {
      name    = "demo-${input.tag}"
      enabled = true
      state   = "running"
    }
  }
}

host "server1" {
  component "alpha" {
    source = component.transport
    inputs = { tag = "alpha" }
  }

  component "beta" {
    source = component.transport
    inputs = { tag = "beta" }
  }
}
