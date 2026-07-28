package graph

import (
	"reflect"
	"testing"
)

func TestCompileNetworkdActivationGraphDeduplicatesAndOrders(t *testing.T) {
	resourceGraph := compileGraphInline(t, `
script "reexport" {
  mode = "once"
  run  = "birdc reload out kernel4"
}

host "server1" {
  systemd {
    networkd {
      network "20-wg0" {
        content = "[Match]\nName=wg0\n"
        activation {
          reconfigure = ["wg1", "wg0"]
          post_reload = script.reexport
        }
      }
      network "21-wg1" {
        content = "[Match]\nName=wg1\n"
        activation {
          reconfigure = ["wg0"]
          post_reload = script.reexport
        }
      }
    }
  }
}
`)
	reloadAddress := "host.server1.systemd.networkd.restart"
	wg0Address := `host.server1.systemd.networkd.reconfigure["wg0"]`
	wg1Address := `host.server1.systemd.networkd.reconfigure["wg1"]`
	firstTrigger := `host.server1.systemd.networkd.network["20-wg0"]`
	secondTrigger := `host.server1.systemd.networkd.network["21-wg1"]`
	postAddress := `host.server1.script["reexport"]`

	wg0 := operationFor(resourceGraph, wg0Address)
	wg1 := operationFor(resourceGraph, wg1Address)
	post := operationFor(resourceGraph, postAddress)
	if wg0 == nil || wg1 == nil || post == nil {
		t.Fatalf("activation operations missing: wg0=%#v wg1=%#v post=%#v", wg0, wg1, post)
	}
	if !reflect.DeepEqual(wg0.TriggeredBy, []string{firstTrigger, secondTrigger}) || !containsString(wg0.DependsOn, reloadAddress) {
		t.Fatalf("wg0 operation = %#v", wg0)
	}
	if !reflect.DeepEqual(wg1.TriggeredBy, []string{firstTrigger}) || !containsString(wg1.DependsOn, wg0Address) {
		t.Fatalf("wg1 operation = %#v", wg1)
	}
	if !reflect.DeepEqual(post.TriggeredBy, []string{firstTrigger, secondTrigger}) || !containsString(post.DependsOn, wg1Address) || post.ScriptPayload == nil || post.ScriptPayload.Mode != "once" {
		t.Fatalf("post-reload operation = %#v", post)
	}
}

func TestCompileNetworkdPostReloadKeepsDeclarationsDistinct(t *testing.T) {
	resourceGraph := compileGraphInline(t, `
script "first" {
  mode = "once"
  run  = "same command"
}
script "second" {
  mode = "once"
  run  = "same command"
}

host "server1" {
  systemd {
    networkd {
      network "20-first" {
        content = "[Match]\nName=first0\n"
        activation {
          post_reload = script.first
        }
      }
      network "21-second" {
        content = "[Match]\nName=second0\n"
        activation {
          post_reload = script.second
        }
      }
    }
  }
}
`)
	if operationFor(resourceGraph, `host.server1.script["first"]`) == nil || operationFor(resourceGraph, `host.server1.script["second"]`) == nil {
		t.Fatalf("distinct post-reload declarations were collapsed: %#v", resourceGraph.Operations)
	}
}

func TestCompileNetworkdPostReloadResolvesGlobalScriptFromComponent(t *testing.T) {
	resourceGraph := compileGraphInline(t, `
script "reexport" {
  mode = "once"
  run  = "birdc reload out kernel4"
}

component "routing" {
  systemd {
    networkd {
      network "20-wg0" {
        content = "[Match]\nName=wg0\n"
        activation {
          post_reload = global.script.reexport
        }
      }
    }
  }
}

host "server1" {
  component "routing" {
    source = component.routing
  }
}
`)
	post := operationFor(resourceGraph, `host.server1.script["reexport"]`)
	trigger := `host.server1.components.routing.systemd.networkd.network["20-wg0"]`
	if post == nil {
		t.Fatalf("global post-reload operation missing: %#v", resourceGraph.Operations)
	}
	if !reflect.DeepEqual(post.TriggeredBy, []string{trigger}) || post.ScriptPayload == nil || post.ScriptPayload.ComponentName != "" {
		t.Fatalf("global post-reload operation = %#v", post)
	}
}
