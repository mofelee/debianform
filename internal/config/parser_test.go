package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadForEachNetworkdFile(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "main.dbf.hcl")
	input := `
state "ssh" {
  host = "server1"
  path = "/var/lib/debianform/state.json"
}

debian_networkd_file "native" {
  for_each = {
    "10-eth0.network" = <<-EOF
      [Match]
      Name=eth0
    EOF
    "20-wg0.netdev" = <<-EOF
      [NetDev]
      Name=wg0
    EOF
  }

  host = "server1"
  name = each.key
  content = each.value
}
`
	if err := os.WriteFile(file, []byte(input), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load([]string{file})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(cfg.Resources), 2; got != want {
		t.Fatalf("len(resources) = %d, want %d", got, want)
	}
	if got, want := cfg.Resources[0].Address, `debian_networkd_file.native["10-eth0.network"]`; got != want {
		t.Fatalf("address = %q, want %q", got, want)
	}
	if got, want := cfg.Resources[0].Attrs["name"], "10-eth0.network"; got != want {
		t.Fatalf("name = %#v, want %q", got, want)
	}
	if got := cfg.Resources[0].Attrs["content"].(string); got != "[Match]\nName=eth0\n" {
		t.Fatalf("unexpected heredoc content: %q", got)
	}
}

func TestLoadResourceRefs(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "main.dbf.hcl")
	input := `
state "ssh" {
  host = "server1"
  path = "/var/lib/debianform/state.json"
}

debian_package "nginx" {
  host = "server1"
}

debian_file "nginx_default" {
  host = "server1"
  path = "/tmp/default"
  content = "ok"
}

debian_service "nginx" {
  host = "server1"
  depends_on = [
    debian_package.nginx,
    debian_file.nginx_default,
  ]
}
`
	if err := os.WriteFile(file, []byte(input), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load([]string{file})
	if err != nil {
		t.Fatal(err)
	}
	service := cfg.Resources[2]
	if got, want := len(service.DependsOn), 2; got != want {
		t.Fatalf("depends_on len = %d, want %d", got, want)
	}
	if got, want := service.DependsOn[0], "debian_package.nginx"; got != want {
		t.Fatalf("dep = %q, want %q", got, want)
	}
}

func TestLoadHandlerNotify(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "main.dbf.hcl")
	input := `
state "ssh" {
  host = "server1"
  path = "/var/lib/debianform/state.json"
}

handler "reload_nginx" {
  host = "server1"
  command = "systemctl reload nginx"
}

debian_file "nginx_default" {
  host = "server1"
  path = "/tmp/default"
  content = "ok"
  notify = [
    handler.reload_nginx,
  ]
}
`
	if err := os.WriteFile(file, []byte(input), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load([]string{file})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(cfg.Handlers), 1; got != want {
		t.Fatalf("len(handlers) = %d, want %d", got, want)
	}
	if got, want := cfg.Handlers[0].Address, "handler.reload_nginx"; got != want {
		t.Fatalf("handler address = %q, want %q", got, want)
	}
	if got, want := cfg.Resources[0].Notify[0], "handler.reload_nginx"; got != want {
		t.Fatalf("notify = %q, want %q", got, want)
	}
}

func TestLoadRejectsUnknownNotifyHandler(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "main.dbf.hcl")
	input := `
state "ssh" {
  host = "server1"
  path = "/var/lib/debianform/state.json"
}

debian_file "nginx_default" {
  host = "server1"
  path = "/tmp/default"
  content = "ok"
  notify = [
    handler.reload_nginx,
  ]
}
`
	if err := os.WriteFile(file, []byte(input), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load([]string{file}); err == nil {
		t.Fatal("Load succeeded, want unknown handler error")
	}
}

func TestLoadForEachLocalToSet(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "main.dbf.hcl")
	input := `
state "ssh" {
  host = "ksvm201"
  path = "/tmp/state.json"
}

locals {
  hosts = toset([
    "ksvm201",
    "ksvm202",
  ])
}

debian_file "host_file" {
  for_each = local.hosts

  host = each.key
  path = "/tmp/${each.key}.txt"
  content = "host ${each.value}\n"
}
`
	if err := os.WriteFile(file, []byte(input), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load([]string{file})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(cfg.Resources), 2; got != want {
		t.Fatalf("len(resources) = %d, want %d", got, want)
	}
	if got, want := cfg.Resources[0].Address, `debian_file.host_file["ksvm201"]`; got != want {
		t.Fatalf("address = %q, want %q", got, want)
	}
	if got, want := cfg.Resources[1].Host, "ksvm202"; got != want {
		t.Fatalf("host = %q, want %q", got, want)
	}
	if got, want := cfg.Resources[1].Attrs["content"], "host ksvm202\n"; got != want {
		t.Fatalf("content = %#v, want %q", got, want)
	}
}

func TestLoadConditionalExpression(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "main.dbf.hcl")
	input := `
state "ssh" {
  host = "ksvm201"
  path = "/tmp/state.json"
}

locals {
  production = true
}

locals {
  service_state = local.production ? "running" : "stopped"
}

debian_service "example" {
  for_each = toset([
    "ci",
    "user",
  ])

  host    = "ksvm201"
  name    = each.key == "ci" ? "ci-service" : "user-service"
  state   = local.service_state
  enabled = each.key != "user"
}
`
	if err := os.WriteFile(file, []byte(input), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load([]string{file})
	if err != nil {
		t.Fatal(err)
	}

	if got, want := cfg.Locals["service_state"], "running"; got != want {
		t.Fatalf("local.service_state = %#v, want %q", got, want)
	}
	if got, want := cfg.Resources[0].Attrs["name"], "ci-service"; got != want {
		t.Fatalf("ci name = %#v, want %q", got, want)
	}
	if got, want := cfg.Resources[0].Attrs["enabled"], true; got != want {
		t.Fatalf("ci enabled = %#v, want %t", got, want)
	}
	if got, want := cfg.Resources[1].Attrs["name"], "user-service"; got != want {
		t.Fatalf("user name = %#v, want %q", got, want)
	}
	if got, want := cfg.Resources[1].Attrs["enabled"], false; got != want {
		t.Fatalf("user enabled = %#v, want %t", got, want)
	}
}

func TestLoadConditionalExpressionInCompositeValues(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "main.dbf.hcl")
	input := `
state "ssh" {
  host = "server1"
  path = "/tmp/state.json"
}

debian_package "example" {
  host = "server1"

  labels = true ? ["primary"] : ["secondary"]
  settings = false ? {
    choice = "primary"
  } : {
    choice = "secondary"
  }
  selected_set = true ? toset(["one"]) : toset(["two"])
  nested       = false ? "one" : true ? "two" : "three"
  parenthesized = (1 == 1.0) ? "equal" : "different"
}
`
	if err := os.WriteFile(file, []byte(input), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load([]string{file})
	if err != nil {
		t.Fatal(err)
	}

	attrs := cfg.Resources[0].Attrs
	labels, ok := attrs["labels"].([]any)
	if !ok || len(labels) != 1 || labels[0] != "primary" {
		t.Fatalf("labels = %#v, want [primary]", attrs["labels"])
	}
	settings, ok := attrs["settings"].(map[string]any)
	if !ok || settings["choice"] != "secondary" {
		t.Fatalf("settings = %#v, want secondary choice", attrs["settings"])
	}
	selectedSet, ok := attrs["selected_set"].([]any)
	if !ok || len(selectedSet) != 1 || selectedSet[0] != "one" {
		t.Fatalf("selected_set = %#v, want [one]", attrs["selected_set"])
	}
	if got, want := attrs["nested"], "two"; got != want {
		t.Fatalf("nested = %#v, want %q", got, want)
	}
	if got, want := attrs["parenthesized"], "equal"; got != want {
		t.Fatalf("parenthesized = %#v, want %q", got, want)
	}
}

func TestParseConditionalRequiresColon(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "main.dbf.hcl")
	input := `
locals {
  value = true ? "yes"
}
`
	if err := os.WriteFile(file, []byte(input), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseFile(file); err == nil || !strings.Contains(err.Error(), `expected ':'`) {
		t.Fatalf("ParseFile() error = %v, want missing colon error", err)
	}
}

func TestLoadNativeSystemResources(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "main.dbf.hcl")
	input := `
state "ssh" {
  host = "server1"
  path = "/var/lib/debianform/state.json"
}

debian_kernel_module "br_netfilter" {
  host = "server1"
  name = "br_netfilter"
  path = "/etc/modules-load.d/kubernetes.conf"
}

debian_sysctl "ip_forward" {
  host = "server1"
  key = "net.ipv4.ip_forward"
  value = "1"
}

debian_nftables_file "main" {
  host = "server1"
  path = "/etc/nftables.conf"
  validate = true
  activate = false

  content = <<-EOF
    flush ruleset
  EOF
}
`
	if err := os.WriteFile(file, []byte(input), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load([]string{file})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(cfg.Resources), 3; got != want {
		t.Fatalf("len(resources) = %d, want %d", got, want)
	}
	if got, want := cfg.Resources[0].Type, "debian_kernel_module"; got != want {
		t.Fatalf("resource type = %q, want %q", got, want)
	}
	if got, want := cfg.Resources[1].Attrs["key"], "net.ipv4.ip_forward"; got != want {
		t.Fatalf("sysctl key = %#v, want %q", got, want)
	}
	if got, want := cfg.Resources[2].Attrs["content"], "flush ruleset\n"; got != want {
		t.Fatalf("nft content = %#v, want %q", got, want)
	}
}
