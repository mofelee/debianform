component "runner" {
  input "tag" {
    type     = string
    nullable = false
  }

  systemd {
    service_unit "worker" {
      name        = "dbf-artifact-${input.tag}"
      description = "DebianForm cross-component artifact runner ${input.tag}"
      run         = ["/usr/local/bin/dbf-artifact-tool", input.tag]

      service_config = {
        ExecStop = "/usr/local/bin/dbf-artifact-tool stop"
      }

      wanted_by = ["multi-user.target"]
    }
  }

  services {
    service "worker" {
      name       = "dbf-artifact-${input.tag}"
      depends_on = [artifact["/usr/local/bin/dbf-artifact-tool"]]
      enabled    = true
      state      = "running"
    }
  }
}
