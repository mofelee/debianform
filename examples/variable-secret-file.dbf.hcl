# Recommended secret-file pattern.
#
# In real use, pass app_token from a runtime source, for example:
#   dbf plan -f examples/variable-secret-file.dbf.hcl -var app_token=@secrets/app-token --offline

variable "app_token" {
  type      = string
  sensitive = true
  ephemeral = true
  default   = "not-a-real-ephemeral-token"
}

variable "app_token_version" {
  type        = string
  default     = "v1"
  description = "Non-sensitive version that triggers updates for write-only app_token content."
}

host "secret_file1" {
  files {
    file "/etc/debianform/app.token" {
      owner           = "root"
      group           = "root"
      mode            = "0600"
      content         = var.app_token
      content_version = var.app_token_version
    }
  }
}
