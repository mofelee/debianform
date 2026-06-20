package graph

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mofelee/debianform/internal/v2/merge"
	"github.com/mofelee/debianform/internal/v2/parser"
)

func TestCompileBBRResourceGraphGolden(t *testing.T) {
	resourceGraph := compileGraphFixture(t, "../../../examples/v2-bbr.dbf.hcl")

	data, err := json.MarshalIndent(resourceGraph, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got := string(data) + "\n"
	assertGolden(t, "../testdata/graph/v2-bbr.golden.json", got)

	dependsOn := dependsOnFor(resourceGraph, `host.bbr1.kernel.sysctl["net.ipv4.tcp_congestion_control"]`)
	want := []string{`host.bbr1.kernel.module["tcp_bbr"]`}
	if strings.Join(dependsOn, "\n") != strings.Join(want, "\n") {
		t.Fatalf("tcp_congestion_control depends_on = %#v, want %#v", dependsOn, want)
	}
}

func TestCompileFoundationResourceGraphGolden(t *testing.T) {
	resourceGraph := compileGraphFixture(t, "../testdata/fixtures/v2-foundation.dbf.hcl")

	data, err := json.MarshalIndent(resourceGraph, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got := string(data) + "\n"
	assertGolden(t, "../testdata/graph/v2-foundation.golden.json", got)

	userDeps := dependsOnFor(resourceGraph, `host.foundation1.users.user["deploy"]`)
	if !containsString(userDeps, `host.foundation1.groups.group["deploy"]`) {
		t.Fatalf("user deps = %#v, want deploy group dependency", userDeps)
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
}

func TestCompileServiceRestartOperation(t *testing.T) {
	resourceGraph := compileGraphInline(t, `
host "server1" {
  systemd {
    unit "worker.service" {
      content = "[Service]\nExecStart=/bin/true\n"
    }
  }

  services {
    service "worker" {
      state = "restarted"
    }
  }
}
`)

	if !hasOperation(resourceGraph, `host.server1.services.service["worker"].restart`) {
		t.Fatalf("restart operation missing: %#v", resourceGraph.Operations)
	}
}

func compileGraphFixture(t *testing.T, fixture string) *ResourceGraph {
	t.Helper()

	cfg, err := parser.ParseFiles([]string{fixture})
	if err != nil {
		t.Fatal(err)
	}
	program, err := merge.Compile(cfg)
	if err != nil {
		t.Fatal(err)
	}
	resourceGraph, err := Compile(program)
	if err != nil {
		t.Fatal(err)
	}
	return resourceGraph
}

func compileGraphInline(t *testing.T, content string) *ResourceGraph {
	t.Helper()

	dir := t.TempDir()
	file := filepath.Join(dir, "main.dbf.hcl")
	if err := os.WriteFile(file, []byte(strings.TrimPrefix(content, "\n")), 0644); err != nil {
		t.Fatal(err)
	}
	return compileGraphFixture(t, file)
}

func dependsOnFor(resourceGraph *ResourceGraph, address string) []string {
	for _, node := range resourceGraph.Nodes {
		if node.Address == address {
			return node.DependsOn
		}
	}
	return nil
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func hasOperation(resourceGraph *ResourceGraph, address string) bool {
	for _, operation := range resourceGraph.Operations {
		if operation.Address == address {
			return true
		}
	}
	return false
}

func assertGolden(t *testing.T, golden string, got string) {
	t.Helper()

	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll(filepath.Dir(golden), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, []byte(got), 0644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Fatalf("golden mismatch\n--- got ---\n%s\n--- want ---\n%s", got, string(want))
	}
}
