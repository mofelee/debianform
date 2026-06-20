# DebianForm v2 binary component 设计示例。
#
# v2 编译器尚未接入当前 CLI；本文件是设计夹具。
#
# 设计边界：
# - component 封装 artifact source、extract 和 install。
# - source 按 target.system.architecture 选择。
# - 下载必须先校验，再解压/安装，失败不能触碰目标路径。

component "rclone" {
  type    = "binary"
  version = "1.66.0"

  source "amd64" {
    url    = "https://downloads.rclone.org/v1.66.0/rclone-v1.66.0-linux-amd64.zip"
    sha256 = "REPLACE_WITH_RCLONE_AMD64_SHA256"
  }

  source "arm64" {
    url    = "https://downloads.rclone.org/v1.66.0/rclone-v1.66.0-linux-arm64.zip"
    sha256 = "REPLACE_WITH_RCLONE_ARM64_SHA256"
  }

  extract {
    format           = "zip"
    strip_components = 1
    include          = "rclone"
  }

  install {
    path  = "/usr/local/bin/rclone"
    owner = "root"
    group = "root"
    mode  = "0755"
  }
}

host "tool1" {
  components = [
    component.rclone,
  ]

  ssh {
    host = "tool1"
  }

  system {
    hostname     = "tool1"
    architecture = "amd64"
    codename     = "trixie"
  }
}

# 预期资源图：
#
# host.tool1.components.rclone.download["amd64"]
#   -> host.tool1.components.rclone.install["/usr/local/bin/rclone"]
