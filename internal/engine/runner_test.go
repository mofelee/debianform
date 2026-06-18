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
