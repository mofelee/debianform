# DebianForm v2 source-build component 示例。
#
# 这个 component 下载源码包，校验 sha256，运行受限的参数数组命令编译，
# 再把 build.output 安装到目标路径。
#
# build.commands 不经过 shell 拼接；每个命令都是 argv 列表。

component "hello_from_source" {
  type    = "source"
  version = "1.0.0"

  source "amd64" {
    url    = "https://downloads.example/hello-from-source-1.0.0.tar.gz"
    sha256 = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
  }

  source "arm64" {
    url    = "https://downloads.example/hello-from-source-1.0.0.tar.gz"
    sha256 = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
  }

  extract {
    format           = "tar.gz"
    strip_components = 1
  }

  build {
    packages = ["gcc"]

    commands = [
      ["cc", "-O2", "-Wall", "-o", "hello-from-source", "hello.c"],
    ]
    output = "hello-from-source"
  }

  install {
    path  = "/usr/local/bin/hello-from-source"
    owner = "root"
    group = "root"
    mode  = "0755"
  }
}

host "build1" {
  components = [
    component.hello_from_source,
  ]

  system {
    architecture = "amd64"
  }
}
