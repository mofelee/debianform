# 用 debianform 从 CZ.NIC 官方仓库安装 BIRD2。
#
# 这是把命令式安装脚本(下载 GPG key → dearmor → 写仓库 → apt update →
# apt install → 启用服务)翻译成声明式资源的例子。资源之间用 depends_on
# 表达先后顺序,dbf 会按依赖拓扑排序后执行。
#
# 使用前:
#   1. 把 host 别名 "bird_host" 换成你的 SSH 主机(可写在 ~/.ssh/config 的 Host)。
#   2. 先把 CZ.NIC 公钥下载到本地(只需一次):
#        mkdir -p examples/keys
#        curl -fsSL https://pkg.labs.nic.cz/gpg -o examples/keys/cznic.asc
#      该文件是 ASCII-armored 公钥。现代 Debian(trixie)的 apt 可以在
#      Signed-By 里直接使用 .asc,无需再 gpg --dearmor 成二进制 keyring。
#   3. dbf plan  -f examples/bird2.dbf.hcl
#      dbf apply -f examples/bird2.dbf.hcl

state "ssh" {
  host      = "bird_host"
  path      = "/var/lib/debianform/bird2-state.json"
  lock_path = "/var/lock/debianform/bird2-state.lock"
}

locals {
  # Debian 13 = trixie。换成目标系统的 codename 即可(例如 bookworm)。
  suite = "trixie"
}

# 1) 访问 HTTPS 仓库所需的根证书(云镜像通常已自带,这里保证其存在)。
debian_package "ca_certificates" {
  host = "bird_host"
  name = "ca-certificates"
}

# 2) 存放仓库签名公钥的目录。
debian_directory "keyrings" {
  host = "bird_host"
  path = "/etc/apt/keyrings"
  mode = "0755"
}

# 3) 写入 CZ.NIC 的 ASCII-armored 公钥(对应脚本里的「下载 + dearmor」两步)。
debian_file "cznic_key" {
  host    = "bird_host"
  path    = "/etc/apt/keyrings/cznic.asc"
  content = file("keys/cznic.asc")
  mode    = "0644"

  depends_on = [
    debian_directory.keyrings,
  ]
}

# 4) 添加 CZ.NIC BIRD2 仓库(deb822 格式,Signed-By 指向上面的公钥)。
debian_apt_source "cznic_bird2" {
  host       = "bird_host"
  uris       = "https://pkg.labs.nic.cz/bird2"
  suites     = local.suite
  components = "main"
  signed_by  = "/etc/apt/keyrings/cznic.asc"

  depends_on = [
    debian_file.cznic_key,
  ]
}

# 5) 安装 bird2。update_cache = true 会在安装前执行 apt-get update,
#    从而读到上一步新增的仓库。
debian_package "bird2" {
  host         = "bird_host"
  name         = "bird2"
  update_cache = true

  depends_on = [
    debian_apt_source.cznic_bird2,
    debian_package.ca_certificates,
  ]
}

# 6) 启用并启动 bird 服务(对应脚本结尾的 systemctl status bird)。
debian_service "bird" {
  host    = "bird_host"
  name    = "bird"
  enabled = true
  state   = "running"

  depends_on = [
    debian_package.bird2,
  ]
}
