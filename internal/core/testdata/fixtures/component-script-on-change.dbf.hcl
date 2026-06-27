component "managed_app" {
  input "service_name" {
    type = string
  }

  script "reload" {
    mode = "once"
    run  = "systemctl reload ${input.service_name}.service"
  }

  files {
    file "/etc/managed-app/config.env" {
      owner = "root"
      group = "root"
      mode  = "0644"

      content = "LISTEN_ADDR=127.0.0.1:8080\n"

      on_change = script.reload
    }
  }
}

host "app1" {
  system {
    architecture = "amd64"
    codename     = "trixie"
  }

  component "app" {
    source = component.managed_app

    inputs = {
      service_name = "managed-app"
    }
  }
}
