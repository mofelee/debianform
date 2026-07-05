# DebianForm Docker users 示例。
#
# docker.users 只声明用户应属于 docker supplementary group，不接管用户 home/shell/uid。
# 已登录 session 需要重新登录后才会获得新的 docker group 权限。

host "docker-users1" {
  groups {
    group "deploy" {
      system = true
    }
  }

  users {
    user "deploy" {
      home  = "/home/deploy"
      shell = "/bin/bash"
      group = "deploy"
    }
  }

  docker {
    enable = true
    users  = ["deploy"]
  }
}
