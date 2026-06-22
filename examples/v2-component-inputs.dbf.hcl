# DebianForm v2 rich component input 示例。
#
# 该示例展示 Terraform-like input type constraint 的第一阶段能力：
# list(object(...))、optional(...)、description 和 nullable。

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
  }

  files {
    file "/etc/reverse-proxy/listeners.json" {
      mode    = "0644"
      content = jsonencode(input.listeners)
    }
  }
}

host "input1" {
  system {
    architecture = "amd64"
    codename     = "trixie"
  }

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
    }
  }
}
