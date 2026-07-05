# DebianForm rich component input 示例。
#
# 该示例展示 Terraform-like component input 能力：
# list(object(...))、optional(...)、description、nullable、validation 和 sensitive 传播。

component "reverse_proxy" {
  input "listeners" {
    type = list(object({
      name = string
      port = number
      note = optional(string)
      tls  = optional(bool, false)
      tags = optional(map(string), {})
    }))

    description = "Reverse proxy listener definitions."
    default     = []
    nullable    = false

    validation {
      condition = alltrue([
        for listener in input.listeners :
        listener.port >= 1 && listener.port <= 65535
      ])
      error_message = "Each listener.port must be between 1 and 65535."
    }
  }

  input "environment" {
    type        = map(string)
    description = "Environment values rendered into the service environment file."
    default     = {}
    sensitive   = true
  }

  files {
    file "/etc/reverse-proxy/listeners.json" {
      mode    = "0644"
      content = jsonencode(input.listeners)
    }

    file "/etc/reverse-proxy/environment.json" {
      mode    = "0600"
      content = jsonencode(input.environment)
    }
  }
}

host "input1" {
  component "proxy" {
    source = component.reverse_proxy

    inputs = {
      listeners = [
        {
          name = "http"
          port = 80
        },
        {
          name = "https"
          port = 443
          tls  = true
          tags = {
            public = "true"
          }
        },
      ]
      environment = {
        API_TOKEN = "example-secret-token"
      }
    }
  }
}
