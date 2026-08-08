package graph

import "testing"

func TestPackageConffilePolicyReachesProviderDesiredState(t *testing.T) {
	resourceGraph := compileGraphInline(t, `
host "server1" {
  packages {
    package "vault" { conffile_policy = "replace" }
  }
}
`)
	node := nodeFor(resourceGraph, `host.server1.packages.install["vault"]`)
	if node == nil || node.Desired["conffile_policy"] != "replace" {
		t.Fatalf("package node = %#v", node)
	}
}
