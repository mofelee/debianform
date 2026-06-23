variable "hostname" {
  type    = string
  default = "vars1"
}

variable "message" {
  type    = string
  default = "hello from variable default"
}

variable "service_description" {
  type    = string
  default = "Variable backed service"
}

component "message_unit" {
  systemd {
    service_unit "message" {
      description = var.service_description
      run         = ["/bin/echo", var.message]
    }
  }
}

profile "variable_base" {
  files {
    file "/etc/debianform/profile-message.txt" {
      content = var.message
    }
  }
}

host "vars1" {
  imports = [profile.variable_base]

  system {
    hostname = var.hostname
  }

  files {
    file "/etc/debianform/message.txt" {
      content = var.message
    }
  }

  components = [component.message_unit]
}
