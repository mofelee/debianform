component "worker" {
  input "environment" {
    type      = map(string)
    sensitive = true
    default   = {}
  }

  systemd {
    service_unit "worker" {
      run = ["/usr/bin/worker"]

      environment = input.environment
    }
  }
}

host "server1" {
  component "worker" {
    source = component.worker

    inputs = {
      environment = {
        API_TOKEN = "not-a-real-service-token"
      }
    }
  }
}
