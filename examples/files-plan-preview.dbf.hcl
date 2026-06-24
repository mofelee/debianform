# Runnable plan preview fixture for text and sensitive diffs.

host "preview1" {
  files {
    file "/etc/debianform/preview.conf" {
      mode = "0644"
      content = <<-EOF
        listen = "127.0.0.1:8080"
        environment = "preview"
      EOF
    }

    # Placeholder only. Never commit a real secret value to configuration.
    file "/etc/debianform/preview.token" {
      mode      = "0600"
      sensitive = true
      content   = "not-a-real-preview-secret"
    }
  }
}
