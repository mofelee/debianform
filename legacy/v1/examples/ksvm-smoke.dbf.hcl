state "ssh" {
  host      = "ksvm201"
  path      = "/tmp/debianform-smoke/state.json"
  lock_path = "/tmp/debianform-smoke/state.lock"
}

debian_directory "smoke_dir" {
  host = "ksvm201"
  path = "/tmp/debianform-smoke"
  mode = "0755"
}

debian_file "smoke_file" {
  host    = "ksvm201"
  path    = "/tmp/debianform-smoke/hello.txt"
  content = "hello from debianform\n"
  mode    = "0644"

  depends_on = [
    debian_directory.smoke_dir,
  ]
}
