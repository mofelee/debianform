# DebianForm Docker official repository mirror 示例。
#
# repository_url 和 gpg_url 只影响 package.source = "official"。
# 自定义 gpg_url 默认不使用 Docker 官方 GPG sha256；需要固定 key 内容时可设置 gpg_sha256。

host "docker-mirror1" {
  system {
    architecture = "amd64"
    codename     = "trixie"
  }

  docker {
    enable = true

    package {
      repository_url = "https://mirrors.aliyun.com/docker-ce/linux/debian"
      gpg_url        = "https://mirrors.aliyun.com/docker-ce/linux/debian/gpg"
    }
  }
}
