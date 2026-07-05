profile "base" {
  packages {
    install = ["curl"]
  }

  groups {
    group "deploy" {
      system = true
    }
  }
}

host "foundation1" {
  imports = [profile.base]

  ssh {
    host = "foundation1"
  }

  system {
    hostname = "foundation1"
  }

  platform {
    architecture = "amd64"
    codename     = "trixie"
  }

  directories {
    directory "/etc/myapp" {
      owner = "root"
      group = "root"
      mode  = "0755"
    }
  }

  users {
    user "deploy" {
      home  = "/home/deploy"
      shell = "/bin/bash"
      group = "deploy"

      ssh_authorized_keys = [
        "ssh-ed25519 AAAATESTKEY deploy@example",
      ]
    }
  }

  files {
    file "/etc/myapp/config.yaml" {
      owner = "root"
      group = "root"
      mode  = "0644"

      content = <<-EOF
        listen: 127.0.0.1:8080
      EOF
    }
  }

  secrets {
    file "/etc/myapp/token" {
      source = "app-token.txt"
    }
  }

  systemd {
    unit "myapp.service" {
      content = <<-EOF
        [Unit]
        Description=My App

        [Service]
        ExecStart=/usr/local/bin/myapp --config /etc/myapp/config.yaml
      EOF
    }
  }

  services {
    service "myapp" {
      package = "curl"
      enabled = true
      state   = "running"
    }
  }
}
