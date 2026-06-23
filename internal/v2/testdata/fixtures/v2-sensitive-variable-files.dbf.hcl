variable "api_token" {
  type      = string
  sensitive = true
  default   = "not-a-real-variable-secret"
}

variable "environment" {
  type    = string
  default = "prod"
}

host "varsecret1" {
  files {
    file "/etc/debianform/token.txt" {
      content   = var.api_token
      sensitive = false
    }

    file "/etc/debianform/config.json" {
      content = jsonencode({
        token = var.api_token
        env   = var.environment
      })
    }

    file "/etc/debianform/template.txt" {
      content = "token=${var.api_token}\nenv=${var.environment}\n"
    }

    file "/etc/debianform/public.txt" {
      content = var.environment
    }
  }

  systemd {
    unit "raw-token.service" {
      content = "[Service]\nEnvironment=API_TOKEN=${var.api_token}\n"
    }

    service_unit "structured-token" {
      run = "/usr/bin/true"
      environment = {
        API_TOKEN = var.api_token
      }
    }
  }
}
