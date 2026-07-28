moved {
  from = component.legacy
  to   = component.current
}

component "managed_file" {
  files {
    file "/etc/moved-component.conf" {
      content = "managed\n"
    }
  }
}

host "server1" {
  component "current" {
    source = component.managed_file
  }
}
