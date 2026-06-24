# DebianForm binary component 示例。
#
# 本文件进入 validate/plan golden 测试；真实 apply 前需要把 sha256 换成
# 上游下载物的真实 digest。
#
# 行为边界：
# - component 封装 artifact source、extract 和 install。
# - source 按 target.system.architecture 选择。
# - 下载必须先校验，再解压/安装，失败不能触碰目标路径。

component "rclone" {
  type    = "binary"
  version = "1.66.0"

  source "amd64" {
    url    = "https://downloads.rclone.org/v1.66.0/rclone-v1.66.0-linux-amd64.zip"
    sha256 = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
  }

  source "arm64" {
    url    = "https://downloads.rclone.org/v1.66.0/rclone-v1.66.0-linux-arm64.zip"
    sha256 = "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
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
}

# 预期资源图：
#
# host.tool1.components.rclone.artifact.download["amd64"]
#   -> host.tool1.components.rclone.artifact.install["/usr/local/bin/rclone"]
