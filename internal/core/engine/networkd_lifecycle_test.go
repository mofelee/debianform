package engine

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"

	"github.com/mofelee/debianform/internal/core/graph"
	corestate "github.com/mofelee/debianform/internal/core/state"
)

func TestRawNetworkdResourceUsesNativeDriftLifecycle(t *testing.T) {
	const content = "[Match]\nName=wg0\n\n[Network]\nAddress=192.0.2.1/32\n"
	node := graph.Node{
		Host: "server1",
		Kind: "networkd_network",
		Desired: map[string]any{
			"path":         "/etc/systemd/network/20-wg0.network",
			"content":      content,
			"content_mode": "raw",
			"owner":        "root",
			"group":        "root",
			"mode":         "0644",
			"ensure":       "present",
		},
	}
	wantSHA := fmt.Sprintf("%x", sha256.Sum256([]byte(content)))
	runner := &recordingRunner{outputs: []Result{
		{Stdout: "file\nroot\nroot\n644\ndrifted-sha\n"},
		{Stdout: "file\nroot\nroot\n644\n" + wantSHA + "\n"},
		{Stdout: "file\nroot\nroot\n644\n" + wantSHA + "\n"},
	}}
	provider := NewNativeProvider(runner)

	drift, err := provider.Plan(context.Background(), node, nil)
	if err != nil {
		t.Fatal(err)
	}
	if drift.Action != ActionUpdate {
		t.Fatalf("raw drift plan = %#v, want update", drift)
	}
	adopt, err := provider.Plan(context.Background(), node, nil)
	if err != nil {
		t.Fatal(err)
	}
	if adopt.Action != ActionAdopt || adopt.Ownership != "adopted" {
		t.Fatalf("raw unmanaged plan = %#v, want adopt", adopt)
	}
	prior := &corestate.Resource{
		Kind:          node.Kind,
		Ownership:     "managed",
		Desired:       cloneMap(node.Desired),
		DesiredDigest: corestate.DesiredDigest(node.Desired),
	}
	converged, err := provider.Plan(context.Background(), node, prior)
	if err != nil {
		t.Fatal(err)
	}
	if converged.Action != ActionNoOp || converged.Ownership != "managed" {
		t.Fatalf("raw managed plan = %#v, want no-op", converged)
	}
}

func TestRawNetworkdPreventDestroyBlocksExplicitDelete(t *testing.T) {
	program, resourceGraph := fixtureProgramAndGraph(t, writeEngineConfig(t, `
host "server1" {
  systemd {
    networkd {
      netdev "10-dummy0" {
        ensure  = "absent"
        content = "[NetDev]\nName=dummy0\nKind=dummy\n"
        lifecycle {
          prevent_destroy = true
        }
      }
    }
  }
}
`))
	provider := NewMemoryProvider()
	address := `host.server1.systemd.networkd.netdev["10-dummy0"]`
	provider.Observed[address] = Observed{Exists: true}
	engine := Engine{Backend: NewMemoryBackend(), Provider: provider}

	_, err := engine.Plan(context.Background(), program, resourceGraph, Options{})
	if err == nil || !strings.Contains(err.Error(), "lifecycle.prevent_destroy") {
		t.Fatalf("plan error = %v, want networkd prevent_destroy", err)
	}
}

func TestCompiledRawAndStructuredNetworkdResourcesUseUbuntuOwnershipPreflight(t *testing.T) {
	_, resourceGraph := fixtureProgramAndGraph(t, writeEngineConfig(t, `
host "server1" {
  systemd {
    networkd {
      netdev "10-dummy0" {
        content = "[NetDev]\nName=dummy0\nKind=dummy\n"
      }
      network "20-dummy0" {
        section "match" {
          name     = "Match"
          settings = { Name = "dummy0" }
        }
        section "network" {
          name     = "Network"
          settings = { Address = "192.0.2.1/32" }
        }
      }
    }
  }
}
`))
	runner := &recordingRunner{outputs: []Result{{}}}
	provider := NewNativeProvider(runner)

	if err := provider.PreflightHost(context.Background(), testPreflightHost("ubuntu"), resourceGraph.Nodes); err != nil {
		t.Fatal(err)
	}
	if len(runner.scripts) != 1 || runner.scripts[0] != netplanOwnershipScript {
		t.Fatalf("ownership preflight calls = %#v", runner.scripts)
	}
}
