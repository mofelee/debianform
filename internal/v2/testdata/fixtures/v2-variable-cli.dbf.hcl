variable "environment" {
  type    = string
  default = "dev"
}

variable "replicas" {
  type    = number
  default = 1
}

variable "enabled" {
  type    = bool
  default = false
}

variable "ports" {
  type    = list(number)
  default = []
}

variable "labels" {
  type = object({
    tier   = string
    canary = optional(bool, false)
  })

  default = {
    tier = "backend"
  }
}

variable "token_seed" {
  type      = number
  sensitive = true
  default   = 0
}

host "cli1" {
  files {
    file "/etc/debianform/cli-vars.json" {
      content = jsonencode({
        environment = var.environment
        replicas    = var.replicas
        enabled     = var.enabled
        ports       = var.ports
        labels      = var.labels
      })
    }
  }
}
