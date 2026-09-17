locals {
  artifact_tool = <<-EOF
    #!/bin/sh
    set -eu
    action="$${1:-run}"
    if [ "$action" = "stop" ]; then
      touch /tmp/dbf-artifact-stop.ok
      exit 0
    fi
    printf 'tag=%s\n' "$action" > "/tmp/dbf-artifact-$${action}.marker"
    exec sleep infinity
  EOF
}

component "artifact_tool" {
  type    = "file"
  version = "1.0.0"

  source {
    url    = "file:///var/lib/debianform-integration/component-artifact-dependency/tool.sh"
    sha256 = "b7b28f7f08df3213255ff001db02bbf514a1c349cc0fb438d8b5fe7a776d0b04"
  }

  install {
    path  = "/usr/local/bin/dbf-artifact-tool"
    owner = "root"
    group = "root"
    mode  = "0755"
  }
}

host "cihost" {
  ssh {
    host          = "__DBF_VM_IP__"
    user          = "root"
    identity_file = "${path.module}/id_ed25519"
  }

  state {
    path      = "/var/lib/debianform-integration/component-artifact-dependency-state.json"
    lock_path = "/var/lock/debianform-integration/component-artifact-dependency.lock"
  }

  directories {
    directory "/var/lib/debianform-integration/component-artifact-dependency" {
      owner = "root"
      group = "root"
      mode  = "0755"
    }
  }

  files {
    file "/var/lib/debianform-integration/component-artifact-dependency/tool.sh" {
      owner   = "root"
      group   = "root"
      mode    = "0755"
      content = local.artifact_tool
    }
  }

  components = [component.artifact_tool]

  component "alpha" {
    source = component.runner
    inputs = { tag = "alpha" }
  }

  component "beta" {
    source = component.runner
    inputs = { tag = "beta" }
  }
}
