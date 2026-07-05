# Component script/on_change example.
#
# This example shows how a component can own both a managed config file and the
# operation that makes the component observe config changes.
#
#   dbf validate -f examples/component-script-on-change.dbf.hcl
#   dbf plan -f examples/component-script-on-change.dbf.hcl --offline

component "managed_app" {
  input "service_name" {
    type        = string
    description = "Systemd service unit name without the .service suffix."
  }

  input "listen_addr" {
    type    = string
    default = "127.0.0.1:8080"
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

      content = "LISTEN_ADDR=${input.listen_addr}\n"

      on_change = script.reload
    }
  }
}

host "app1" {
  component "app" {
    source = component.managed_app

    inputs = {
      service_name = "managed-app"
      listen_addr  = "127.0.0.1:9000"
    }
  }
}
