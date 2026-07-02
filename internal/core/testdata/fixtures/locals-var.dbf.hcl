variable "firecrawl_public_port" {
  type    = string
  default = "3002"
}

variable "postgres_password" {
  type      = string
  sensitive = true
}

variable "bull_auth_key" {
  type      = string
  sensitive = true
}

locals {
  firecrawl_env = <<-ENV
    PORT=${var.firecrawl_public_port}
    POSTGRES_PASSWORD=${var.postgres_password}
    BULL_AUTH_KEY=${var.bull_auth_key}
  ENV
}

host "localsvar1" {
  system {
    architecture = "amd64"
    codename     = "trixie"
  }

  docker {
    enable = true

    compose "firecrawl" {
      directory = "/opt/firecrawl"

      file {
        path    = "/opt/firecrawl/compose.yaml"
        content = "services: {}\n"
      }

      env_file "app" {
        path    = "/opt/firecrawl/.env"
        content = local.firecrawl_env
        mode    = "0600"
      }
    }
  }
}
