package graph

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCompileBBRResourceGraphGolden(t *testing.T) {
	resourceGraph := compileGraphFixture(t, "../../../examples/bbr.dbf.hcl")

	data, err := json.MarshalIndent(resourceGraph, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got := string(data) + "\n"
	assertGolden(t, "../testdata/graph/bbr.golden.json", got)

	dependsOn := dependsOnFor(resourceGraph, `host.bbr1.kernel.sysctl["net.ipv4.tcp_congestion_control"]`)
	want := []string{`host.bbr1.kernel.module["tcp_bbr"]`}
	if strings.Join(dependsOn, "\n") != strings.Join(want, "\n") {
		t.Fatalf("tcp_congestion_control depends_on = %#v, want %#v", dependsOn, want)
	}
}

func TestCompileFoundationResourceGraphGolden(t *testing.T) {
	resourceGraph := compileGraphFixture(t, "../testdata/fixtures/foundation.dbf.hcl")

	data, err := json.MarshalIndent(resourceGraph, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got := string(data) + "\n"
	assertGolden(t, "../testdata/graph/foundation.golden.json", got)

	userDeps := dependsOnFor(resourceGraph, `host.foundation1.users.user["deploy"]`)
	if !containsString(userDeps, `host.foundation1.groups.group["deploy"]`) {
		t.Fatalf("user deps = %#v, want deploy group dependency", userDeps)
	}
	directoryDeps := dependsOnFor(resourceGraph, `host.foundation1.directories.directory["/etc/myapp"]`)
	if len(directoryDeps) != 0 {
		t.Fatalf("root-owned directory deps = %#v, want none", directoryDeps)
	}
	serviceDeps := dependsOnFor(resourceGraph, `host.foundation1.services.service["myapp"]`)
	for _, want := range []string{
		`host.foundation1.packages.install["curl"]`,
		`host.foundation1.systemd.unit["myapp.service"]`,
		`host.foundation1.systemd.daemon_reload`,
	} {
		if !containsString(serviceDeps, want) {
			t.Fatalf("service deps = %#v, want %q", serviceDeps, want)
		}
	}
	for _, operation := range resourceGraph.Operations {
		if operation.Host != "foundation1" {
			t.Fatalf("operation %s host = %q, want foundation1", operation.Address, operation.Host)
		}
	}
}

func TestCompileSystemHostnameResourceRequiresExplicitHostname(t *testing.T) {
	empty := compileGraphInline(t, `host "web1" {}`)
	if node := nodeFor(empty, "host.web1.system.hostname"); node != nil {
		t.Fatalf("implicit hostname generated system hostname node: %#v", node)
	}
	if node := nodeFor(empty, "host.web1.system.timezone"); node != nil {
		t.Fatalf("implicit timezone generated system timezone node: %#v", node)
	}
	if node := nodeFor(empty, "host.web1.system.locale"); node != nil {
		t.Fatalf("implicit locale generated system locale node: %#v", node)
	}

	resourceGraph := compileGraphInline(t, `
host "web1" {
  system {
    hostname = "web-01"
    timezone = "Asia/Shanghai"
    locale   = "en_US.UTF-8"
  }
}
`)
	node := nodeFor(resourceGraph, "host.web1.system.hostname")
	if node == nil {
		t.Fatal("explicit hostname did not generate system hostname node")
	}
	if node.Kind != "system_hostname" || node.ProviderType != "system_hostname" || node.ProviderAddress != "system_hostname.web1" {
		t.Fatalf("hostname node provider = kind:%s type:%s address:%s", node.Kind, node.ProviderType, node.ProviderAddress)
	}
	if node.Desired["hostname"] != "web-01" || node.ProviderPayload["hostname"] != "web-01" {
		t.Fatalf("hostname desired/payload = %#v / %#v, want web-01", node.Desired, node.ProviderPayload)
	}
	timezone := nodeFor(resourceGraph, "host.web1.system.timezone")
	if timezone == nil {
		t.Fatal("explicit timezone did not generate system timezone node")
	}
	if timezone.Kind != "system_timezone" || timezone.ProviderType != "system_timezone" || timezone.ProviderAddress != "system_timezone.web1" {
		t.Fatalf("timezone node provider = kind:%s type:%s address:%s", timezone.Kind, timezone.ProviderType, timezone.ProviderAddress)
	}
	if timezone.Desired["timezone"] != "Asia/Shanghai" || timezone.ProviderPayload["timezone"] != "Asia/Shanghai" {
		t.Fatalf("timezone desired/payload = %#v / %#v, want Asia/Shanghai", timezone.Desired, timezone.ProviderPayload)
	}
	locale := nodeFor(resourceGraph, "host.web1.system.locale")
	if locale == nil {
		t.Fatal("explicit locale did not generate system locale node")
	}
	if locale.Kind != "system_locale" || locale.ProviderType != "system_locale" || locale.ProviderAddress != "system_locale.web1" {
		t.Fatalf("locale node provider = kind:%s type:%s address:%s", locale.Kind, locale.ProviderType, locale.ProviderAddress)
	}
	if locale.Desired["locale"] != "en_US.UTF-8" || locale.ProviderPayload["locale"] != "en_US.UTF-8" {
		t.Fatalf("locale desired/payload = %#v / %#v, want en_US.UTF-8", locale.Desired, locale.ProviderPayload)
	}
}

func TestCompileProfileMergeResourceGraphGolden(t *testing.T) {
	resourceGraph := compileGraphFixture(t, "../../../examples/profile-merge.dbf.hcl")

	data, err := json.MarshalIndent(resourceGraph, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got := string(data) + "\n"
	assertGolden(t, "../testdata/graph/profile-merge.golden.json", got)

	for _, want := range []string{
		`host.merge1.packages.install["curl"]`,
		`host.merge1.packages.install["vim"]`,
		`host.merge1.packages.install["htop"]`,
		`host.merge1.packages.install["git"]`,
		`host.merge1.kernel.module["tcp_bbr"]`,
	} {
		if nodeFor(resourceGraph, want) == nil {
			t.Fatalf("resource graph missing %q", want)
		}
	}
}
