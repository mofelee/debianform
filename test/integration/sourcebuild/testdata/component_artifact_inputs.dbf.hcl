variable "base_url" {
  type     = string
  nullable = false
  default  = "https://example.invalid"
}

variable "binary_sha256" {
  type     = string
  nullable = false
  default  = "1111111111111111111111111111111111111111111111111111111111111111"
}

variable "archive_sha256" {
  type     = string
  nullable = false
  default  = "2222222222222222222222222222222222222222222222222222222222222222"
}

variable "source_sha256" {
  type     = string
  nullable = false
  default  = "3333333333333333333333333333333333333333333333333333333333333333"
}

component "input_binary" {
  input "download_url" {
    type     = string
    nullable = false
  }

  input "download_sha256" {
    type     = string
    nullable = false
  }

  type = "binary"

  source "amd64" {
    url    = input.download_url
    sha256 = input.download_sha256
  }

  install {
    path = "/tmp/debianform-input-binary"
  }
}

component "input_archive" {
  input "download_url" {
    type     = string
    nullable = false
  }

  input "download_sha256" {
    type     = string
    nullable = false
  }

  type = "archive"

  source "amd64" {
    url    = input.download_url
    sha256 = input.download_sha256
  }

  extract {
    format = "tar.gz"
  }

  install {
    path = "/tmp/debianform-input-archive"
  }
}

component "input_source" {
  input "download_url" {
    type     = string
    nullable = false
  }

  input "download_sha256" {
    type     = string
    nullable = false
  }

  type = "source"

  source "amd64" {
    url    = input.download_url
    sha256 = input.download_sha256
  }

  build {
    commands    = [["true"]]
    output      = "unused"
    source_name = "source.txt"
  }

  install {
    path = "/tmp/debianform-input-source"
  }
}

host "mirror_a" {
  platform {
    architecture = "amd64"
  }

  component "binary" {
    source = component.input_binary
    inputs = {
      download_url    = "${var.base_url}/mirror-a/binary"
      download_sha256 = var.binary_sha256
    }
  }

  component "archive" {
    source = component.input_archive
    inputs = {
      download_url    = "${var.base_url}/mirror-a/archive"
      download_sha256 = var.archive_sha256
    }
  }

  component "source" {
    source = component.input_source
    inputs = {
      download_url    = "${var.base_url}/mirror-a/source"
      download_sha256 = var.source_sha256
    }
  }
}

host "mirror_b" {
  platform {
    architecture = "amd64"
  }

  component "binary" {
    source = component.input_binary
    inputs = {
      download_url    = "${var.base_url}/mirror-b/binary"
      download_sha256 = var.binary_sha256
    }
  }

  component "archive" {
    source = component.input_archive
    inputs = {
      download_url    = "${var.base_url}/mirror-b/archive"
      download_sha256 = var.archive_sha256
    }
  }

  component "source" {
    source = component.input_source
    inputs = {
      download_url    = "${var.base_url}/mirror-b/source"
      download_sha256 = var.source_sha256
    }
  }
}
