locals {
  shared_prefix = "shared=base-profile"
}

variable "message" {
  type    = string
  default = "default"
}

variable "file_mode" {
  type    = string
  default = "0644"
}
