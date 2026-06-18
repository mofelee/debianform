state "ssh" {
  host      = "ksvm213"
  path      = "/var/lib/debianform/bbr-state.json"
  lock_path = "/var/lock/debianform/bbr-state.lock"
}

debian_kernel_module "tcp_bbr" {
  host    = "ksvm213"
  name    = "tcp_bbr"
  persist = true
  path    = "/etc/modules-load.d/bbr.conf"
}

debian_sysctl "bbr_qdisc" {
  host  = "ksvm213"
  key   = "net.core.default_qdisc"
  value = "fq"
  path  = "/etc/sysctl.d/90-dbf-bbr-qdisc.conf"

  depends_on = [
    debian_kernel_module.tcp_bbr,
  ]
}

debian_sysctl "bbr_congestion_control" {
  host  = "ksvm213"
  key   = "net.ipv4.tcp_congestion_control"
  value = "bbr"
  path  = "/etc/sysctl.d/90-dbf-bbr-congestion.conf"

  depends_on = [
    debian_kernel_module.tcp_bbr,
  ]
}
