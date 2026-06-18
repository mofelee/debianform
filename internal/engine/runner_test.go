package engine

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/mofelee/debianform/internal/config"
	"github.com/mofelee/debianform/internal/sshx"
)

// fakeRunner is an in-memory engine.Runner. It records every script it is
// asked to run and returns whatever reply matches, so plan/apply logic can be
// tested without a live SSH connection.
type fakeRunner struct {
	scripts []string
	reply   func(host, script string) (sshx.Result, error)
}

func (f *fakeRunner) Run(_ context.Context, host, script string) (sshx.Result, error) {
	f.scripts = append(f.scripts, script)
	if f.reply != nil {
		return f.reply(host, script)
	}
	return sshx.Result{}, nil
}

func (f *fakeRunner) RunCommand(ctx context.Context, host, remoteCommand string) (sshx.Result, error) {
	return f.Run(ctx, host, remoteCommand+"\n")
}

func planFixture(t *testing.T, runner Runner, res config.Resource) Change {
	t.Helper()
	e := &Engine{runner: runner}
	change, err := e.planResource(context.Background(), res)
	if err != nil {
		t.Fatalf("planResource: %v", err)
	}
	return change
}

func serviceResource() config.Resource {
	return config.Resource{
		Type:    "debian_service",
		Name:    "nginx",
		Address: "debian_service.nginx",
		Host:    "server1",
		Attrs: map[string]any{
			"name":    "nginx",
			"enabled": true,
			"state":   "running",
		},
	}
}

func TestPlanServiceReportsUpdateWhenDisabledAndStopped(t *testing.T) {
	runner := &fakeRunner{
		reply: func(_, _ string) (sshx.Result, error) {
			return sshx.Result{Stdout: "enabled=disabled\nactive=inactive\n"}, nil
		},
	}

	change := planFixture(t, runner, serviceResource())

	if change.Action != "update" {
		t.Fatalf("action = %q, want update", change.Action)
	}
	if !strings.Contains(change.Summary, "enable") || !strings.Contains(change.Summary, "start") {
		t.Fatalf("summary = %q, want enable + start", change.Summary)
	}
	if len(runner.scripts) != 1 || !strings.Contains(runner.scripts[0], "systemctl is-enabled") {
		t.Fatalf("expected an is-enabled probe, got %q", runner.scripts)
	}
}

func TestPlanServiceIsNoOpWhenAlreadyEnabledAndRunning(t *testing.T) {
	runner := &fakeRunner{
		reply: func(_, _ string) (sshx.Result, error) {
			return sshx.Result{Stdout: "enabled=enabled\nactive=active\n"}, nil
		},
	}

	if got := planFixture(t, runner, serviceResource()).Action; got != "no-op" {
		t.Fatalf("action = %q, want no-op", got)
	}
}

func TestPlanServicePropagatesRunnerError(t *testing.T) {
	wantErr := errors.New("ssh exploded")
	runner := &fakeRunner{
		reply: func(_, _ string) (sshx.Result, error) {
			return sshx.Result{}, wantErr
		},
	}

	e := &Engine{runner: runner}
	if _, err := e.planResource(context.Background(), serviceResource()); !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want %v", err, wantErr)
	}
}

func TestPlanSysctlComparesRuntimeValue(t *testing.T) {
	res := config.Resource{
		Type:    "debian_sysctl",
		Name:    "ip_forward",
		Address: "debian_sysctl.ip_forward",
		Host:    "server1",
		Attrs: map[string]any{
			"key":     "net.ipv4.ip_forward",
			"value":   "1",
			"persist": false,
		},
	}

	cases := map[string]struct {
		stdout     string
		wantAction string
	}{
		"matches":   {stdout: "1\n", wantAction: "no-op"},
		"different": {stdout: "0\n", wantAction: "update"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			runner := &fakeRunner{
				reply: func(_, _ string) (sshx.Result, error) {
					return sshx.Result{Stdout: tc.stdout}, nil
				},
			}
			if got := planFixture(t, runner, res).Action; got != tc.wantAction {
				t.Fatalf("action = %q, want %q", got, tc.wantAction)
			}
		})
	}
}

func TestPlanPackageFromDpkgQuery(t *testing.T) {
	present := config.Resource{
		Type: "debian_package", Name: "curl", Address: "debian_package.curl", Host: "server1",
		Attrs: map[string]any{"name": "curl"},
	}
	absent := config.Resource{
		Type: "debian_package", Name: "nginx", Address: "debian_package.nginx", Host: "server1",
		Attrs: map[string]any{"name": "nginx", "ensure": "absent"},
	}

	cases := map[string]struct {
		res        config.Resource
		stdout     string
		wantAction string
	}{
		"not installed":        {res: present, stdout: "\n", wantAction: "create"},
		"already installed":    {res: present, stdout: "install ok installed\t8.5.0\n", wantAction: "no-op"},
		"absent but installed": {res: absent, stdout: "install ok installed\t1.24\n", wantAction: "delete"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			runner := &fakeRunner{reply: func(_, _ string) (sshx.Result, error) {
				return sshx.Result{Stdout: tc.stdout}, nil
			}}
			got := planFixture(t, runner, tc.res)
			if got.Action != tc.wantAction {
				t.Fatalf("action = %q, want %q", got.Action, tc.wantAction)
			}
			if !strings.Contains(runner.scripts[0], "dpkg-query") {
				t.Fatalf("expected a dpkg-query probe, got %q", runner.scripts[0])
			}
		})
	}
}

func TestPlanFileComparesContentAndMetadata(t *testing.T) {
	res := config.Resource{
		Type: "debian_file", Name: "motd", Address: "debian_file.motd", Host: "server1",
		Attrs: map[string]any{
			"path": "/etc/motd", "content": "hello\n",
			"owner": "root", "group": "root", "mode": "0644",
		},
	}
	matchingState := "file\nroot\nroot\n644\n" + hash("hello\n") + "\n"

	cases := map[string]struct {
		readPath   string
		wantAction string
	}{
		"absent":        {readPath: "missing\n", wantAction: "create"},
		"in sync":       {readPath: matchingState, wantAction: "no-op"},
		"content drift": {readPath: "file\nroot\nroot\n644\n" + hash("other\n") + "\n", wantAction: "update"},
		"mode drift":    {readPath: "file\nroot\nroot\n600\n" + hash("hello\n") + "\n", wantAction: "update"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			runner := &fakeRunner{reply: func(_, _ string) (sshx.Result, error) {
				return sshx.Result{Stdout: tc.readPath}, nil
			}}
			if got := planFixture(t, runner, res).Action; got != tc.wantAction {
				t.Fatalf("action = %q, want %q", got, tc.wantAction)
			}
		})
	}
}

func TestPlanDirectoryHandlesEnsureAbsent(t *testing.T) {
	managed := config.Resource{
		Type: "debian_directory", Name: "app", Address: "debian_directory.app", Host: "server1",
		Attrs: map[string]any{"path": "/var/lib/app", "owner": "root", "group": "root", "mode": "0750"},
	}
	absent := config.Resource{
		Type: "debian_directory", Name: "old", Address: "debian_directory.old", Host: "server1",
		Attrs: map[string]any{"path": "/var/lib/old", "ensure": "absent"},
	}

	cases := map[string]struct {
		res        config.Resource
		readPath   string
		wantAction string
	}{
		"create":         {res: managed, readPath: "missing\n", wantAction: "create"},
		"in sync":        {res: managed, readPath: "dir\nroot\nroot\n750\n\n", wantAction: "no-op"},
		"delete present": {res: absent, readPath: "dir\nroot\nroot\n755\n\n", wantAction: "delete"},
		"delete missing": {res: absent, readPath: "missing\n", wantAction: "no-op"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			runner := &fakeRunner{reply: func(_, _ string) (sshx.Result, error) {
				return sshx.Result{Stdout: tc.readPath}, nil
			}}
			if got := planFixture(t, runner, tc.res).Action; got != tc.wantAction {
				t.Fatalf("action = %q, want %q", got, tc.wantAction)
			}
		})
	}
}

func TestPlanKernelModuleChecksRuntimeLoad(t *testing.T) {
	res := config.Resource{
		Type: "debian_kernel_module", Name: "br_netfilter", Address: "debian_kernel_module.br_netfilter", Host: "server1",
		Attrs: map[string]any{"name": "br_netfilter", "persist": false},
	}

	cases := map[string]struct {
		probe      string
		wantAction string
	}{
		"loaded":  {probe: "loaded\n", wantAction: "no-op"},
		"missing": {probe: "missing\n", wantAction: "update"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			runner := &fakeRunner{reply: func(_, script string) (sshx.Result, error) {
				if strings.Contains(script, "/proc/modules") {
					return sshx.Result{Stdout: tc.probe}, nil
				}
				t.Fatalf("unexpected non-probe script with persist=false: %q", script)
				return sshx.Result{}, nil
			}}
			if got := planFixture(t, runner, res).Action; got != tc.wantAction {
				t.Fatalf("action = %q, want %q", got, tc.wantAction)
			}
		})
	}
}
