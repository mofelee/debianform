package engine

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mofelee/debianform/internal/core/graph"
	corestate "github.com/mofelee/debianform/internal/core/state"
)

func TestNativeProviderSystemHostnamePlanApplyAndNoOp(t *testing.T) {
	node := graph.Node{
		Address: "host.web1.system.hostname",
		Host:    "web1",
		Kind:    "system_hostname",
		Desired: map[string]any{"hostname": "web-01"},
	}
	runner := &recordingRunner{outputs: []Result{
		{Stdout: "old-host\n"},
		{},
		{Stdout: "web-01\n"},
		{Stdout: "web-01\n"},
	}}
	provider := NewNativeProvider(runner)

	got, err := provider.Plan(context.Background(), node, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Action != ActionUpdate {
		t.Fatalf("hostname drift action = %q, want update", got.Action)
	}
	if got.Observed["hostname"] != "old-host" {
		t.Fatalf("hostname observed = %#v, want old-host", got.Observed)
	}

	observed, err := provider.Apply(context.Background(), Step{Address: node.Address, Node: node, Action: ActionUpdate})
	if err != nil {
		t.Fatal(err)
	}
	if observed["hostname"] != "web-01" || observed["desired_digest"] != corestate.DesiredDigest(node.Desired) {
		t.Fatalf("apply observed = %#v, want hostname and desired digest", observed)
	}
	allScripts := strings.Join(runner.scripts, "\n---\n")
	for _, want := range []string{
		"hostnamectl --static",
		"sed -n '1p' /etc/hostname",
		`hostnamectl set-hostname "$name"`,
		`printf '%s\n' "$name" > /etc/hostname`,
	} {
		if !strings.Contains(allScripts, want) {
			t.Fatalf("system hostname scripts missing %q:\n%s", want, allScripts)
		}
	}
	if strings.Contains(allScripts, "/etc/hosts") {
		t.Fatalf("system hostname provider must not touch /etc/hosts:\n%s", allScripts)
	}

	prior := &corestate.Resource{Ownership: "managed", DesiredDigest: corestate.DesiredDigest(node.Desired)}
	got, err = provider.Plan(context.Background(), node, prior)
	if err != nil {
		t.Fatal(err)
	}
	if got.Action != ActionNoOp {
		t.Fatalf("matching hostname action = %q, want no-op", got.Action)
	}
}

func TestNativeProviderSystemTimezonePlanApplyAndNoOp(t *testing.T) {
	node := graph.Node{
		Address: "host.web1.system.timezone",
		Host:    "web1",
		Kind:    "system_timezone",
		Desired: map[string]any{"timezone": "Asia/Shanghai"},
	}
	runner := &recordingRunner{outputs: []Result{
		{Stdout: "timezone=UTC\nzone_exists=true\n"},
		{},
		{Stdout: "timezone=Asia/Shanghai\nzone_exists=true\n"},
		{Stdout: "timezone=Asia/Shanghai\nzone_exists=true\n"},
	}}
	provider := NewNativeProvider(runner)

	got, err := provider.Plan(context.Background(), node, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Action != ActionUpdate {
		t.Fatalf("timezone drift action = %q, want update", got.Action)
	}
	if got.Observed["timezone"] != "UTC" || got.Observed["zone_exists"] != true {
		t.Fatalf("timezone observed = %#v, want UTC and zone_exists", got.Observed)
	}

	observed, err := provider.Apply(context.Background(), Step{Address: node.Address, Node: node, Action: ActionUpdate})
	if err != nil {
		t.Fatal(err)
	}
	if observed["timezone"] != "Asia/Shanghai" || observed["desired_digest"] != corestate.DesiredDigest(node.Desired) {
		t.Fatalf("apply observed = %#v, want timezone and desired digest", observed)
	}
	allScripts := strings.Join(runner.scripts, "\n---\n")
	for _, want := range []string{
		"timedatectl show -p Timezone --value",
		`timedatectl set-timezone "$tz"`,
		"/usr/share/zoneinfo/$tz",
		"/etc/timezone",
	} {
		if !strings.Contains(allScripts, want) {
			t.Fatalf("system timezone scripts missing %q:\n%s", want, allScripts)
		}
	}

	prior := &corestate.Resource{Ownership: "managed", DesiredDigest: corestate.DesiredDigest(node.Desired)}
	got, err = provider.Plan(context.Background(), node, prior)
	if err != nil {
		t.Fatal(err)
	}
	if got.Action != ActionNoOp {
		t.Fatalf("matching timezone action = %q, want no-op", got.Action)
	}
}

func TestNativeProviderSystemTimezoneRejectsMissingZoneinfo(t *testing.T) {
	node := graph.Node{
		Address: "host.web1.system.timezone",
		Host:    "web1",
		Kind:    "system_timezone",
		Desired: map[string]any{"timezone": "Mars/Base"},
	}
	runner := &recordingRunner{outputs: []Result{{Stdout: "timezone=UTC\nzone_exists=false\n"}}}
	provider := NewNativeProvider(runner)

	_, err := provider.Plan(context.Background(), node, nil)
	if err == nil || !strings.Contains(err.Error(), "/usr/share/zoneinfo/Mars/Base") {
		t.Fatalf("Plan error = %v, want missing zoneinfo", err)
	}
}

func TestNativeProviderSystemLocalePlanApplyAndNoOp(t *testing.T) {
	node := graph.Node{
		Address: "host.web1.system.locale",
		Host:    "web1",
		Kind:    "system_locale",
		Desired: map[string]any{"locale": "en_US.UTF-8"},
	}
	runner := &recordingRunner{outputs: []Result{
		{Stdout: "locale=C\navailable=false\n"},
		{},
		{Stdout: "locale=en_US.UTF-8\navailable=true\n"},
		{Stdout: "locale=en_US.UTF-8\navailable=true\n"},
	}}
	provider := NewNativeProvider(runner)

	got, err := provider.Plan(context.Background(), node, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Action != ActionUpdate {
		t.Fatalf("locale drift action = %q, want update", got.Action)
	}
	if got.Observed["locale"] != "C" || got.Observed["available"] != false {
		t.Fatalf("locale observed = %#v, want C and unavailable", got.Observed)
	}

	observed, err := provider.Apply(context.Background(), Step{Address: node.Address, Node: node, Action: ActionUpdate})
	if err != nil {
		t.Fatal(err)
	}
	if observed["locale"] != "en_US.UTF-8" || observed["desired_digest"] != corestate.DesiredDigest(node.Desired) {
		t.Fatalf("apply observed = %#v, want locale and desired digest", observed)
	}
	allScripts := strings.Join(runner.scripts, "\n---\n")
	for _, want := range []string{
		"apt-get install -y locales",
		`locale-gen "$loc"`,
		"/etc/default/locale",
		`/^LANG=/`,
	} {
		if !strings.Contains(allScripts, want) {
			t.Fatalf("system locale scripts missing %q:\n%s", want, allScripts)
		}
	}

	prior := &corestate.Resource{Ownership: "managed", DesiredDigest: corestate.DesiredDigest(node.Desired)}
	got, err = provider.Plan(context.Background(), node, prior)
	if err != nil {
		t.Fatal(err)
	}
	if got.Action != ActionNoOp {
		t.Fatalf("matching locale action = %q, want no-op", got.Action)
	}
}

func TestSystemSettingScriptsAreShellSyntaxValid(t *testing.T) {
	scripts := map[string]string{
		"system hostname read":  systemHostnameReadScript(),
		"system hostname apply": systemHostnameApplyScript("web-01"),
		"system timezone read":  systemTimezoneReadScript("Asia/Shanghai"),
		"system timezone apply": systemTimezoneApplyScript("Asia/Shanghai"),
		"system locale read":    systemLocaleReadScript("en_US.UTF-8"),
		"system locale apply":   systemLocaleApplyScript("en_US.UTF-8"),
		"system c locale read":  systemLocaleReadScript("C.UTF-8"),
		"system c locale apply": systemLocaleApplyScript("C.UTF-8"),
	}
	for name, script := range scripts {
		t.Run(name, func(t *testing.T) {
			cmd := exec.Command("sh", "-n")
			cmd.Stdin = strings.NewReader(script)
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("script failed sh -n: %v\n%s\nscript:\n%s", err, output, script)
			}
		})
	}
}

func TestSystemLocaleReadScriptDetectsGeneratedLocale(t *testing.T) {
	tests := []struct {
		name          string
		localeOutput  string
		wantAvailable bool
	}{
		{
			name:          "missing",
			localeOutput:  "C\nC.UTF-8\n",
			wantAvailable: false,
		},
		{
			name:          "matching normalized alias",
			localeOutput:  "C\nen_US.utf8\n",
			wantAvailable: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			binDir := t.TempDir()
			localePath := filepath.Join(binDir, "locale")
			fakeLocale := "#!/bin/sh\nif [ \"$1\" = \"-a\" ]; then\ncat <<'DBF_LOCALES'\n" + tt.localeOutput + "DBF_LOCALES\nfi\n"
			if err := os.WriteFile(localePath, []byte(fakeLocale), 0755); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command("sh", "-s")
			cmd.Stdin = strings.NewReader(systemLocaleReadScript("en_US.UTF-8"))
			cmd.Env = append(os.Environ(), "PATH="+binDir+":"+os.Getenv("PATH"))
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("script failed: %v\n%s", err, output)
			}
			values := parseFactLines(string(output))
			got := values["available"] == "true"
			if got != tt.wantAvailable {
				t.Fatalf("available = %v, want %v\noutput:\n%s", got, tt.wantAvailable, output)
			}
		})
	}
}
