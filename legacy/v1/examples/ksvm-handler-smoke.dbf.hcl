state "ssh" {
  host      = "ksvm201"
  path      = "/tmp/debianform-handler-smoke/state.json"
  lock_path = "/tmp/debianform-handler-smoke/state.lock"
}

handler "record_change" {
  host    = "ksvm201"
  command = "date -u +%Y-%m-%dT%H:%M:%SZ >> /tmp/debianform-handler-smoke/handler.log"
}

debian_directory "smoke_dir" {
  host = "ksvm201"
  path = "/tmp/debianform-handler-smoke"
  mode = "0755"
}

debian_file "smoke_file" {
  host    = "ksvm201"
  path    = "/tmp/debianform-handler-smoke/hello.txt"
  content = "handler smoke v2\n"
  mode    = "0644"

  depends_on = [
    debian_directory.smoke_dir,
  ]

  notify = [
    handler.record_change,
  ]
}
