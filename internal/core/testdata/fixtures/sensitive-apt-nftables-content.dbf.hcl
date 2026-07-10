variable "managed_content" {
  type      = string
  sensitive = true
  default   = "not-a-real-variable-secret"
}

component "private_apt" {
  input "managed_content" {
    type      = string
    sensitive = true
  }

  apt {
    source_file "component-private" {
      path    = "/etc/apt/sources.list.d/component-private.list"
      content = input.managed_content
    }

    repository "component-private" {
      uris       = ["https://component.repo.example/debian"]
      suites     = ["trixie"]
      components = ["main"]

      signing_key {
        path    = "/etc/apt/keyrings/component-private.asc"
        content = input.managed_content
      }
    }
  }
}

host "server1" {
  apt {
    source_file "private" {
      path    = "/etc/apt/sources.list.d/private.list"
      content = var.managed_content
    }

    repository "private" {
      uris       = ["https://repo.example/debian"]
      suites     = ["trixie"]
      components = ["main"]

      signing_key {
        path    = "/etc/apt/keyrings/private.asc"
        content = var.managed_content
      }
    }
  }

  nftables {
    main {
      content = var.managed_content
    }

    file "private" {
      path      = "/etc/nftables.d/private.nft"
      content   = var.managed_content
      sensitive = false
    }
  }

  component "private_apt" {
    source = component.private_apt
    inputs = {
      managed_content = var.managed_content
    }
  }
}
