host "cihost" {
  ssh {
    host          = "__DBF_VM_IP__"
    user          = "root"
    identity_file = "${path.module}/id_ed25519"
  }

  state {
    path      = "/var/lib/debianform-integration/docker-compose-state.json"
    lock_path = "/var/lock/debianform-integration/docker-compose-state.lock"
  }

  docker {
    enable = true

    compose "app" {
      state     = "running"
      directory = "/opt/debianform-compose-app"

      file {
        path = "/opt/debianform-compose-app/compose.yaml"

        content = <<-YAML
          services:
            web:
              image: busybox:1.36
              command: ["sh", "-c", "while true; do echo debianform-compose-updated; sleep 60; done"]
              labels:
                com.example.debianform: "loop8-updated"
        YAML
      }

      env_file "app" {
        path    = "/opt/debianform-compose-app/.env"
        content = "TOKEN=loop8-secret-value\n"
        mode    = "0600"
      }
    }
  }
}
