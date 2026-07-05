component "reverse_proxy" {
  input "listeners" {
    type = list(object({
      name = string
      port = number
      note = optional(string)
      tls  = optional(bool, false)
      tags = optional(map(string), {})
    }))

    nullable = false

    validation {
      condition = alltrue([
        for listener in input.listeners :
        listener.port >= 1 && listener.port <= 65535
      ])
      error_message = "Each listener.port must be between 1 and 65535."
    }
  }

  input "environment" {
    type      = map(string)
    default   = {}
    sensitive = true
  }

  files {
    file "/etc/debianform-component-inputs/listeners.json" {
      mode    = "0644"
      content = jsonencode(input.listeners)
    }

    file "/etc/debianform-component-inputs/environment.json" {
      mode    = "0600"
      content = jsonencode(input.environment)
    }
  }
}

host "cihost" {
  ssh {
    host          = "__DBF_VM_IP__"
    user          = "root"
    identity_file = "${path.module}/id_ed25519"
  }

  state {
    path      = "/var/lib/debianform-integration/component-inputs-state.json"
    lock_path = "/var/lock/debianform-integration/component-inputs-state.lock"
  }

  component "proxy" {
    source = component.reverse_proxy

    inputs = {
      listeners = [
        {
          name = "http"
          port = 80
        },
      ]
      environment = {
        API_TOKEN = "component-input-secret"
      }
    }
  }
}
