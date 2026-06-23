variable "runtime_token" {
  type      = string
  sensitive = true
  ephemeral = true
  default   = "not-a-real-ephemeral-token"
}

variable "content_version" {
  type    = string
  default = "v1"
}

host "ephemeral1" {
  files {
    file "/etc/debianform/runtime-token.txt" {
      content = var.runtime_token
    }

    file "/etc/debianform/runtime-token.json" {
      content = jsonencode({
        token   = var.runtime_token
        version = var.content_version
      })
    }
  }
}
