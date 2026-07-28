package merge

import (
	"reflect"
	"testing"
)

func TestCompileProjectsMovedPrefixesToEveryHost(t *testing.T) {
	program := compileInline(t, `
moved {
  from = component.old
  to   = component.current
}

host "server1" {}
host "server2" {}
`)

	if len(program.Hosts) != 2 {
		t.Fatalf("hosts = %d, want 2", len(program.Hosts))
	}
	for _, host := range program.Hosts {
		if len(host.Moves) != 1 {
			t.Fatalf("host %s moves = %#v", host.Name, host.Moves)
		}
		move := host.Moves[0]
		wantFrom := "host." + host.Name + ".components.old"
		wantTo := "host." + host.Name + ".components.current"
		if move.From != wantFrom || move.To != wantTo {
			t.Fatalf("host %s move = %#v, want %s -> %s", host.Name, move, wantFrom, wantTo)
		}
	}
}

func TestCompileHostFilterLimitsMovedProjection(t *testing.T) {
	cfg := parseInline(t, `
moved {
  from = component.old
  to   = component.current
}

host "server1" {}
host "server2" {}
`)
	program, err := CompileWithOptions(cfg, CompileOptions{HostFilter: "server2"})
	if err != nil {
		t.Fatal(err)
	}
	if len(program.Hosts) != 1 || program.Hosts[0].Name != "server2" {
		t.Fatalf("hosts = %#v", program.Hosts)
	}
	want := []string{"host.server2.components.old", "host.server2.components.current"}
	got := []string{program.Hosts[0].Moves[0].From, program.Hosts[0].Moves[0].To}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("move endpoints = %#v, want %#v", got, want)
	}
}

func TestCompileMovedHostSpecGolden(t *testing.T) {
	assertHostSpecGolden(t, "../testdata/fixtures/moved-component.dbf.hcl", "../testdata/hostspec/moved-component.golden.json")
}
