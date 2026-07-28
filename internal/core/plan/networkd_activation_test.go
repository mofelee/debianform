package plan

import (
	"bytes"
	"encoding/json"
	"html"
	"strings"
	"testing"
)

func TestNetworkdActivationRelationshipsAppearInEveryPlanFormat(t *testing.T) {
	const (
		fileAddress        = `host.router.systemd.networkd.network["wg0"]`
		reloadAddress      = "host.router.systemd.networkd.restart"
		reconfigureAddress = `host.router.systemd.networkd.reconfigure["wg0"]`
		postReloadAddress  = `host.router.script["reexport"]`
	)
	doc := Document{
		FormatVersion: FormatVersion,
		Summary:       Summary{Operations: 3},
		Operations: []OperationNode{
			{
				Host:        "router",
				Address:     reloadAddress,
				Action:      "run",
				DependsOn:   []string{fileAddress},
				TriggeredBy: []string{fileAddress},
			},
			{
				Host:        "router",
				Address:     reconfigureAddress,
				Action:      "run",
				DependsOn:   []string{fileAddress, reloadAddress},
				TriggeredBy: []string{fileAddress},
			},
			{
				Host:        "router",
				Address:     postReloadAddress,
				Action:      "run",
				DependsOn:   []string{fileAddress, reconfigureAddress},
				TriggeredBy: []string{fileAddress},
			},
		},
	}

	var textOut bytes.Buffer
	PrintText(&textOut, doc)
	assertNetworkdActivationPlanRelationships(t, "text", textOut.String(), []string{
		"depends_on:", "triggered_by:", fileAddress, reloadAddress, reconfigureAddress, postReloadAddress,
	})

	var jsonOut bytes.Buffer
	if err := PrintJSON(&jsonOut, doc); err != nil {
		t.Fatal(err)
	}
	var decoded Document
	if err := json.Unmarshal(jsonOut.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Operations) != 3 || len(decoded.Operations[2].DependsOn) != 2 || len(decoded.Operations[2].TriggeredBy) != 1 {
		t.Fatalf("JSON relationships = %#v", decoded.Operations)
	}
	assertNetworkdActivationPlanRelationships(t, "JSON", jsonOut.String(), []string{
		`"depends_on"`, `"triggered_by"`, `host.router.systemd.networkd.network[\"wg0\"]`, reloadAddress,
		`host.router.systemd.networkd.reconfigure[\"wg0\"]`, `host.router.script[\"reexport\"]`,
	})

	var htmlOut bytes.Buffer
	if err := PrintHTML(&htmlOut, doc); err != nil {
		t.Fatal(err)
	}
	assertNetworkdActivationPlanRelationships(t, "HTML", html.UnescapeString(htmlOut.String()), []string{
		"depends_on:", "triggered_by:", fileAddress, reloadAddress, reconfigureAddress, postReloadAddress,
	})
}

func assertNetworkdActivationPlanRelationships(t *testing.T, format, output string, values []string) {
	t.Helper()
	for _, value := range values {
		if !strings.Contains(output, value) {
			t.Fatalf("%s plan missing %q:\n%s", format, value, output)
		}
	}
}
