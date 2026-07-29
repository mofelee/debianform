component "binary_tool" {
  input "download_url" {
    type     = string
    nullable = false
  }

  type    = "binary"
  version = "1.0.0"

  source "amd64" {
    url    = input.download_url
    sha256 = "1111111111111111111111111111111111111111111111111111111111111111"
  }

  extract {}

  install {
    path = "/usr/local/bin/input-binary"
  }
}

component "archive_tool" {
  input "download_url" {
    type     = string
    nullable = false
  }

  type    = "archive"
  version = "2.0.0"

  source "amd64" {
    url    = input.download_url
    sha256 = "2222222222222222222222222222222222222222222222222222222222222222"
  }

  extract {
    format           = "tar.gz"
    strip_components = 1
  }

  install {
    path = "/opt/input-archive"
  }
}

component "source_tool" {
  input "download_url" {
    type     = string
    nullable = false
  }

  type    = "source"
  version = "3.0.0"

  source "amd64" {
    url    = input.download_url
    sha256 = "3333333333333333333333333333333333333333333333333333333333333333"
  }

  extract {
    format = "tar.gz"
  }

  build {
    commands = [["make"]]
    output   = "bin/input-source"
  }

  install {
    path = "/usr/local/bin/input-source"
  }
}

component "private_tool" {
  input "download_url" {
    type      = string
    nullable  = false
    sensitive = true
  }

  input "download_sha256" {
    type      = string
    nullable  = false
    sensitive = true
  }

  type    = "binary"
  version = "4.0.0"

  source "amd64" {
    url    = input.download_url
    sha256 = input.download_sha256
  }

  install {
    path = "/usr/local/bin/private-input-binary"
  }
}

host "mirror_a" {
  platform {
    architecture = "amd64"
  }

  component "binary" {
    source = component.binary_tool
    inputs = {
      download_url = "https://mirror-a.example.invalid/input-binary.gz"
    }
  }

  component "archive" {
    source = component.archive_tool
    inputs = {
      download_url = "https://mirror-a.example.invalid/input-archive.tar.gz"
    }
  }

  component "source" {
    source = component.source_tool
    inputs = {
      download_url = "https://mirror-a.example.invalid/input-source.tar.gz"
    }
  }

  component "private" {
    source = component.private_tool
    inputs = {
      download_url    = "https://not-a-real-variable-secret@mirror-a.example.invalid/private-tool"
      download_sha256 = "5555555555555555555555555555555555555555555555555555555555555555"
    }
  }
}

host "mirror_b" {
  platform {
    architecture = "amd64"
  }

  component "binary" {
    source = component.binary_tool
    inputs = {
      download_url = "https://mirror-b.example.invalid/input-binary.gz"
    }
  }

  component "archive" {
    source = component.archive_tool
    inputs = {
      download_url = "https://mirror-b.example.invalid/input-archive.tar.gz"
    }
  }

  component "source" {
    source = component.source_tool
    inputs = {
      download_url = "https://mirror-b.example.invalid/input-source.tar.gz"
    }
  }

  component "private" {
    source = component.private_tool
    inputs = {
      download_url    = "https://not-a-real-variable-secret@mirror-b.example.invalid/private-tool"
      download_sha256 = "6666666666666666666666666666666666666666666666666666666666666666"
    }
  }
}
