variable "environment" {
  type        = string
  description = "Deployment environment."
  default     = "prod"
  nullable    = false

  validation {
    condition     = contains(["dev", "staging", "prod"], var.environment)
    error_message = "environment must be dev, staging, or prod."
  }
}

variable "listeners" {
  type = list(object({
    name = string
    port = number
    tls  = optional(bool, false)
  }))

  default = [
    {
      name = "http"
      port = 80
    },
  ]
}

variable "app_token" {
  type        = string
  description = "Placeholder token used only to test redaction."
  default     = "not-a-real-variable-secret"
  sensitive   = true
  ephemeral   = true
}

host "vars1" {}
