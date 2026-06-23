locals {
  hello_source = <<-EOF
    #include <stdio.h>

    int main(void) {
      puts("hello from debianform source build");
      return 0;
    }
  EOF
}

component "hello_from_source" {
  type    = "source"
  version = "1.0.0"

  source {
    url    = "file:///var/lib/debianform-integration/source-build/hello.c"
    sha256 = "016b93ed93437aeb37eb3fb86f3680b7301a4240e59b55d84e7b86c6688ab1b9"
  }

  build {
    packages = ["gcc"]

    commands = [
      ["cc", "-O2", "-Wall", "-o", "hello-from-source", "hello.c"],
    ]
    output      = "hello-from-source"
    source_name = "hello.c"
  }

  install {
    path  = "/usr/local/bin/hello-from-source"
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
    path      = "/var/lib/debianform-integration/source-build-state.json"
    lock_path = "/var/lock/debianform-integration/source-build-state.lock"
  }

  directories {
    directory "/var/lib/debianform-integration/source-build" {
      ensure = "absent"
    }
  }

  files {
    file "/var/lib/debianform-integration/source-build/hello.c" {
      ensure = "absent"
    }
  }
}
