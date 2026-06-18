package engine

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/mofelee/debianform/internal/config"
	"github.com/mofelee/debianform/internal/sshx"
	"github.com/mofelee/debianform/internal/state"
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

func TestPlanGroupFromGetent(t *testing.T) {
	withGID := config.Resource{
		Type: "debian_group", Name: "deploy", Address: "debian_group.deploy", Host: "server1",
		Attrs: map[string]any{"name": "deploy", "gid": "1500"},
	}
	noGID := config.Resource{
		Type: "debian_group", Name: "deploy", Address: "debian_group.deploy", Host: "server1",
		Attrs: map[string]any{"name": "deploy"},
	}
	absent := config.Resource{
		Type: "debian_group", Name: "old", Address: "debian_group.old", Host: "server1",
		Attrs: map[string]any{"name": "old", "ensure": "absent"},
	}

	cases := map[string]struct {
		res        config.Resource
		getent     string
		wantAction string
	}{
		"create when missing": {res: withGID, getent: "\n", wantAction: "create"},
		"gid matches":         {res: withGID, getent: "deploy:x:1500:\n", wantAction: "no-op"},
		"gid drift":           {res: withGID, getent: "deploy:x:1400:alice\n", wantAction: "update"},
		"present without gid": {res: noGID, getent: "deploy:x:1400:\n", wantAction: "no-op"},
		"absent but present":  {res: absent, getent: "old:x:2000:\n", wantAction: "delete"},
		"absent and missing":  {res: absent, getent: "\n", wantAction: "no-op"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			runner := &fakeRunner{reply: func(_, _ string) (sshx.Result, error) {
				return sshx.Result{Stdout: tc.getent}, nil
			}}
			got := planFixture(t, runner, tc.res)
			if got.Action != tc.wantAction {
				t.Fatalf("action = %q, want %q", got.Action, tc.wantAction)
			}
			if !strings.Contains(runner.scripts[0], "getent group") {
				t.Fatalf("expected a getent group probe, got %q", runner.scripts[0])
			}
		})
	}
}

func TestApplyGroupEmitsExpectedCommands(t *testing.T) {
	cases := map[string]struct {
		res         config.Resource
		wantSubstrs []string
	}{
		"system group with gid": {
			res: config.Resource{
				Type: "debian_group", Name: "svc", Address: "debian_group.svc", Host: "server1",
				Attrs: map[string]any{"name": "svc", "gid": "1600", "system": true},
			},
			wantSubstrs: []string{"groupadd -r -g '1600' 'svc'", "groupmod -g '1600' 'svc'"},
		},
		"delete": {
			res: config.Resource{
				Type: "debian_group", Name: "svc", Address: "debian_group.svc", Host: "server1",
				Attrs: map[string]any{"name": "svc", "ensure": "absent"},
			},
			wantSubstrs: []string{"groupdel 'svc'"},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			runner := &fakeRunner{}
			e := &Engine{runner: runner}
			d, err := groupProvider{}.Desired(tc.res)
			if err != nil {
				t.Fatal(err)
			}
			if err := (groupProvider{}).Apply(context.Background(), e, change(tc.res, d, "create", "")); err != nil {
				t.Fatal(err)
			}
			if len(runner.scripts) != 1 {
				t.Fatalf("expected one script, got %d", len(runner.scripts))
			}
			for _, want := range tc.wantSubstrs {
				if !strings.Contains(runner.scripts[0], want) {
					t.Fatalf("script %q missing %q", runner.scripts[0], want)
				}
			}
		})
	}
}

func TestPlanUserFromGetent(t *testing.T) {
	managed := config.Resource{
		Type: "debian_user", Name: "deployer", Address: "debian_user.deployer", Host: "server1",
		Attrs: map[string]any{
			"name": "deployer", "uid": "1500", "gid": "deploy",
			"home": "/home/deployer", "shell": "/bin/bash", "groups": []any{"sudo", "docker"},
		},
	}
	absent := config.Resource{
		Type: "debian_user", Name: "old", Address: "debian_user.old", Host: "server1",
		Attrs: map[string]any{"name": "old", "ensure": "absent"},
	}

	// getent passwd \n id -gn \n id -nG
	inSync := "deployer:x:1500:1500:,,,:/home/deployer:/bin/bash\ndeploy\ndeploy docker sudo\n"
	shellDrift := "deployer:x:1500:1500:,,,:/home/deployer:/bin/sh\ndeploy\ndeploy docker sudo\n"
	groupDrift := "deployer:x:1500:1500:,,,:/home/deployer:/bin/bash\ndeploy\ndeploy sudo\n"

	cases := map[string]struct {
		res        config.Resource
		passwd     string
		wantAction string
	}{
		"create when missing": {res: managed, passwd: "__ABSENT__\n", wantAction: "create"},
		"in sync":             {res: managed, passwd: inSync, wantAction: "no-op"},
		"shell drift":         {res: managed, passwd: shellDrift, wantAction: "update"},
		"supplementary drift": {res: managed, passwd: groupDrift, wantAction: "update"},
		"absent but present":  {res: absent, passwd: "old:x:2000:2000::/home/old:/bin/sh\nold\nold\n", wantAction: "delete"},
		"absent and missing":  {res: absent, passwd: "__ABSENT__\n", wantAction: "no-op"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			runner := &fakeRunner{reply: func(_, _ string) (sshx.Result, error) {
				return sshx.Result{Stdout: tc.passwd}, nil
			}}
			got := planFixture(t, runner, tc.res)
			if got.Action != tc.wantAction {
				t.Fatalf("action = %q, want %q", got.Action, tc.wantAction)
			}
			if !strings.Contains(runner.scripts[0], "getent passwd") {
				t.Fatalf("expected a getent passwd probe, got %q", runner.scripts[0])
			}
		})
	}
}

func TestApplyUserEmitsExpectedCommands(t *testing.T) {
	cases := map[string]struct {
		res         config.Resource
		wantSubstrs []string
	}{
		"create with attributes": {
			res: config.Resource{
				Type: "debian_user", Name: "deployer", Address: "debian_user.deployer", Host: "server1",
				Attrs: map[string]any{
					"name": "deployer", "uid": "1500", "gid": "deploy",
					"home": "/home/deployer", "shell": "/bin/bash", "groups": []any{"sudo", "docker"},
				},
			},
			wantSubstrs: []string{
				"useradd -m -u '1500' -g 'deploy' -G 'sudo,docker' -d '/home/deployer' -s '/bin/bash' 'deployer'",
				"usermod -u '1500' -g 'deploy' -G 'sudo,docker' -d '/home/deployer' -s '/bin/bash' 'deployer'",
			},
		},
		"system user no home": {
			res: config.Resource{
				Type: "debian_user", Name: "svc", Address: "debian_user.svc", Host: "server1",
				Attrs: map[string]any{"name": "svc", "system": true, "shell": "/usr/sbin/nologin"},
			},
			wantSubstrs: []string{"useradd -r -s '/usr/sbin/nologin' 'svc'"},
		},
		"delete": {
			res: config.Resource{
				Type: "debian_user", Name: "svc", Address: "debian_user.svc", Host: "server1",
				Attrs: map[string]any{"name": "svc", "ensure": "absent"},
			},
			wantSubstrs: []string{"userdel 'svc'"},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			runner := &fakeRunner{}
			e := &Engine{runner: runner}
			d, err := userProvider{}.Desired(tc.res)
			if err != nil {
				t.Fatal(err)
			}
			if err := (userProvider{}).Apply(context.Background(), e, change(tc.res, d, "create", "")); err != nil {
				t.Fatal(err)
			}
			for _, want := range tc.wantSubstrs {
				if !strings.Contains(runner.scripts[0], want) {
					t.Fatalf("script %q missing %q", runner.scripts[0], want)
				}
			}
		})
	}
}

func TestPlanAuthorizedKey(t *testing.T) {
	present := config.Resource{
		Type: "debian_authorized_key", Name: "app", Address: "debian_authorized_key.app", Host: "server1",
		Attrs: map[string]any{"user": "deployer", "key": "ssh-ed25519 AAAAC3Nz deploy@ci"},
	}
	absent := config.Resource{
		Type: "debian_authorized_key", Name: "app", Address: "debian_authorized_key.app", Host: "server1",
		Attrs: map[string]any{"user": "deployer", "key": "ssh-ed25519 AAAAC3Nz deploy@ci", "ensure": "absent"},
	}

	cases := map[string]struct {
		res        config.Resource
		stdout     string
		wantAction string
	}{
		"create when missing": {res: present, stdout: "absent\n", wantAction: "create"},
		"present in sync":     {res: present, stdout: "present\n", wantAction: "no-op"},
		"absent but present":  {res: absent, stdout: "present\n", wantAction: "delete"},
		"absent and missing":  {res: absent, stdout: "absent\n", wantAction: "no-op"},
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
			if !strings.Contains(runner.scripts[0], "authorized_keys") {
				t.Fatalf("expected authorized_keys probe, got %q", runner.scripts[0])
			}
		})
	}
}

func TestApplyAuthorizedKeyEmitsExpectedCommands(t *testing.T) {
	present := config.Resource{
		Type: "debian_authorized_key", Name: "app", Address: "debian_authorized_key.app", Host: "server1",
		Attrs: map[string]any{"user": "deployer", "key": "ssh-ed25519 AAAAC3Nz deploy@ci"},
	}
	absent := config.Resource{
		Type: "debian_authorized_key", Name: "app", Address: "debian_authorized_key.app", Host: "server1",
		Attrs: map[string]any{"user": "deployer", "key": "ssh-ed25519 AAAAC3Nz deploy@ci", "ensure": "absent"},
	}

	cases := map[string]struct {
		res         config.Resource
		wantSubstrs []string
	}{
		"create": {res: present, wantSubstrs: []string{
			"chmod 0700 \"$dir\"",
			"printf '%s\\n' 'ssh-ed25519 AAAAC3Nz deploy@ci' >> \"$file\"",
			"chmod 0600 \"$file\"",
			"chown 'deployer':\"$group\" \"$dir\" \"$file\"",
		}},
		"delete": {res: absent, wantSubstrs: []string{
			"!($1==t && $2==b)",
			"cat \"$tmp\" > \"$file\"",
		}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			runner := &fakeRunner{}
			e := &Engine{runner: runner}
			d, err := authorizedKeyProvider{}.Desired(tc.res)
			if err != nil {
				t.Fatal(err)
			}
			if err := (authorizedKeyProvider{}).Apply(context.Background(), e, change(tc.res, d, "create", "")); err != nil {
				t.Fatal(err)
			}
			for _, want := range tc.wantSubstrs {
				if !strings.Contains(runner.scripts[0], want) {
					t.Fatalf("script %q missing %q", runner.scripts[0], want)
				}
			}
		})
	}
}

func TestAuthorizedKeyDesiredRejectsMalformedKey(t *testing.T) {
	res := config.Resource{
		Type: "debian_authorized_key", Name: "app", Address: "debian_authorized_key.app", Host: "server1",
		Attrs: map[string]any{"user": "deployer", "key": "not-a-real-key"},
	}
	if _, err := (authorizedKeyProvider{}).Desired(res); err == nil {
		t.Fatal("expected error for a key without a body")
	}
}

func TestPlanHostname(t *testing.T) {
	res := config.Resource{
		Type: "debian_hostname", Name: "main", Address: "debian_hostname.main", Host: "server1",
		Attrs: map[string]any{"hostname": "web01"},
	}

	cases := map[string]struct {
		current    string
		wantAction string
	}{
		"matches": {current: "web01\n", wantAction: "no-op"},
		"drift":   {current: "localhost\n", wantAction: "update"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			runner := &fakeRunner{reply: func(_, _ string) (sshx.Result, error) {
				return sshx.Result{Stdout: tc.current}, nil
			}}
			got := planFixture(t, runner, res)
			if got.Action != tc.wantAction {
				t.Fatalf("action = %q, want %q", got.Action, tc.wantAction)
			}
			if !strings.Contains(runner.scripts[0], "hostnamectl --static") {
				t.Fatalf("expected hostnamectl probe, got %q", runner.scripts[0])
			}
		})
	}
}

func TestApplyHostnameEmitsSetHostname(t *testing.T) {
	res := config.Resource{
		Type: "debian_hostname", Name: "main", Address: "debian_hostname.main", Host: "server1",
		Attrs: map[string]any{"hostname": "web01"},
	}
	runner := &fakeRunner{}
	e := &Engine{runner: runner}
	d, err := hostnameProvider{}.Desired(res)
	if err != nil {
		t.Fatal(err)
	}
	if err := (hostnameProvider{}).Apply(context.Background(), e, change(res, d, "update", "")); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(runner.scripts[0], "hostnamectl set-hostname 'web01'") {
		t.Fatalf("script %q missing set-hostname", runner.scripts[0])
	}
}

func aptSourceResource() config.Resource {
	return config.Resource{
		Type: "debian_apt_source", Name: "example", Address: "debian_apt_source.example", Host: "server1",
		Attrs: map[string]any{
			"uris": "https://example.invalid/debian", "suites": "trixie",
			"components": "main", "signed_by": "/etc/apt/keyrings/example.gpg",
		},
	}
}

func TestAptSourceDesiredRendersDeb822(t *testing.T) {
	d, err := aptSourceProvider{}.Desired(aptSourceResource())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Types: deb\n",
		"URIs: https://example.invalid/debian\n",
		"Suites: trixie\n",
		"Components: main\n",
		"Signed-By: /etc/apt/keyrings/example.gpg\n",
	} {
		if !strings.Contains(d.Content, want) {
			t.Fatalf("content %q missing %q", d.Content, want)
		}
	}
	if d.Path != "/etc/apt/sources.list.d/example.sources" {
		t.Fatalf("path = %q", d.Path)
	}
}

func TestPlanAptSource(t *testing.T) {
	res := aptSourceResource()
	d, err := aptSourceProvider{}.Desired(res)
	if err != nil {
		t.Fatal(err)
	}
	inSync := "file\nroot\nroot\n644\n" + d.ContentSHA256 + "\n"

	absent := aptSourceResource()
	absent.Attrs["ensure"] = "absent"

	cases := map[string]struct {
		res        config.Resource
		readPath   string
		wantAction string
	}{
		"create when missing": {res: res, readPath: "missing\n", wantAction: "create"},
		"in sync":             {res: res, readPath: inSync, wantAction: "no-op"},
		"content drift":       {res: res, readPath: "file\nroot\nroot\n644\n" + hash("stale") + "\n", wantAction: "update"},
		"absent but present":  {res: absent, readPath: "file\nroot\nroot\n644\n" + hash("x") + "\n", wantAction: "delete"},
		"absent and missing":  {res: absent, readPath: "missing\n", wantAction: "no-op"},
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

func TestApplyAptSource(t *testing.T) {
	t.Run("present writes file", func(t *testing.T) {
		res := aptSourceResource()
		runner := &fakeRunner{}
		e := &Engine{runner: runner}
		d, err := aptSourceProvider{}.Desired(res)
		if err != nil {
			t.Fatal(err)
		}
		if err := (aptSourceProvider{}).Apply(context.Background(), e, change(res, d, "create", "")); err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"base64 -d", "/etc/apt/sources.list.d/example.sources", "install -o 'root' -g 'root' -m '0644'"} {
			if !strings.Contains(runner.scripts[0], want) {
				t.Fatalf("script %q missing %q", runner.scripts[0], want)
			}
		}
	})
	t.Run("absent removes file", func(t *testing.T) {
		res := aptSourceResource()
		res.Attrs["ensure"] = "absent"
		runner := &fakeRunner{}
		e := &Engine{runner: runner}
		d, err := aptSourceProvider{}.Desired(res)
		if err != nil {
			t.Fatal(err)
		}
		if err := (aptSourceProvider{}).Apply(context.Background(), e, change(res, d, "delete", "")); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(runner.scripts[0], "rm -f -- '/etc/apt/sources.list.d/example.sources'") {
			t.Fatalf("script %q missing rm", runner.scripts[0])
		}
	})
}

func aptRepositoryResource() config.Resource {
	return config.Resource{
		Type: "debian_apt_repository", Name: "cznic_bird2", Address: "debian_apt_repository.cznic_bird2", Host: "server1",
		Attrs: map[string]any{
			"uris": "https://pkg.labs.nic.cz/bird2", "suites": "trixie", "components": "main",
			"key": map[string]any{
				"url":  "https://pkg.labs.nic.cz/gpg",
				"path": "/etc/apt/keyrings/cznic.asc",
			},
		},
	}
}

func TestAptRepositoryDesiredRendersSourceWithKey(t *testing.T) {
	d, err := aptRepositoryProvider{}.Desired(aptRepositoryResource())
	if err != nil {
		t.Fatal(err)
	}
	if got, want := d.Path, "/etc/apt/sources.list.d/cznic_bird2.sources"; got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
	if got, want := d.KeyPath, "/etc/apt/keyrings/cznic.asc"; got != want {
		t.Fatalf("key path = %q, want %q", got, want)
	}
	if got, want := d.KeyURL, "https://pkg.labs.nic.cz/gpg"; got != want {
		t.Fatalf("key url = %q, want %q", got, want)
	}
	for _, want := range []string{
		"URIs: https://pkg.labs.nic.cz/bird2\n",
		"Suites: trixie\n",
		"Signed-By: /etc/apt/keyrings/cznic.asc\n",
	} {
		if !strings.Contains(d.Content, want) {
			t.Fatalf("content %q missing %q", d.Content, want)
		}
	}
}

func TestPlanAptRepository(t *testing.T) {
	res := aptRepositoryResource()
	d, err := aptRepositoryProvider{}.Desired(res)
	if err != nil {
		t.Fatal(err)
	}
	inSyncSource := "file\nroot\nroot\n644\n" + d.ContentSHA256 + "\n"
	inSyncKey := "file\nroot\nroot\n644\n\n"

	cases := map[string]struct {
		source     string
		key        string
		wantAction string
	}{
		"create when missing": {source: "missing\n", key: "missing\n", wantAction: "create"},
		"in sync":             {source: inSyncSource, key: inSyncKey, wantAction: "no-op"},
		"source drift":        {source: "file\nroot\nroot\n644\n" + hash("stale") + "\n", key: inSyncKey, wantAction: "update"},
		"key missing":         {source: inSyncSource, key: "missing\n", wantAction: "update"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			calls := 0
			runner := &fakeRunner{reply: func(_, _ string) (sshx.Result, error) {
				calls++
				if calls == 1 {
					return sshx.Result{Stdout: tc.source}, nil
				}
				return sshx.Result{Stdout: tc.key}, nil
			}}
			got, err := aptRepositoryProvider{}.Plan(context.Background(), &Engine{runner: runner}, res, d)
			if err != nil {
				t.Fatal(err)
			}
			if got.Action != tc.wantAction {
				t.Fatalf("action = %q, want %q", got.Action, tc.wantAction)
			}
		})
	}
}

func TestApplyAptRepositoryWithHTTPSKeyURL(t *testing.T) {
	res := aptRepositoryResource()
	runner := &fakeRunner{}
	e := &Engine{runner: runner}
	d, err := aptRepositoryProvider{}.Desired(res)
	if err != nil {
		t.Fatal(err)
	}
	if err := (aptRepositoryProvider{}).Apply(context.Background(), e, change(res, d, "update", "")); err != nil {
		t.Fatal(err)
	}
	if len(runner.scripts) != 1 {
		t.Fatalf("scripts = %d, want 1", len(runner.scripts))
	}
	script := runner.scripts[0]
	for _, want := range []string{
		"apt-get install -y ca-certificates",
		"https://pkg.labs.nic.cz/gpg",
		"/etc/apt/keyrings/cznic.asc",
		"/etc/apt/sources.list.d/cznic_bird2.sources",
		"apt-get update",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("script %q missing %q", script, want)
		}
	}
}

func TestOrphanDestroysOrderAndFiltering(t *testing.T) {
	prior := state.State{Resources: map[string]state.ResourceState{
		"debian_group.deploy": {"type": "debian_group", "host": "h", "name": "deploy", "order": float64(1)},
		"debian_user.app":     {"type": "debian_user", "host": "h", "name": "app", "order": float64(2)},
		"debian_file.keep":    {"type": "debian_file", "host": "h", "path": "/keep", "order": float64(0)},
		"handler.note":        {"type": "handler", "host": "h"},
	}}
	managed := map[string]struct{}{"debian_file.keep": {}}

	changes := orphanDestroys(prior, managed, Options{})

	if len(changes) != 2 {
		t.Fatalf("got %d destroy changes, want 2 (%+v)", len(changes), changes)
	}
	// Higher apply order is destroyed first: user (2) before group (1).
	if changes[0].Address != "debian_user.app" || changes[0].Action != "destroy" {
		t.Fatalf("first = %+v, want destroy debian_user.app", changes[0])
	}
	if changes[1].Address != "debian_group.deploy" {
		t.Fatalf("second = %+v, want debian_group.deploy", changes[1])
	}
}

func TestOrphanDestroysRespectsHostFilter(t *testing.T) {
	prior := state.State{Resources: map[string]state.ResourceState{
		"debian_group.a": {"type": "debian_group", "host": "h1", "name": "a"},
		"debian_group.b": {"type": "debian_group", "host": "h2", "name": "b"},
	}}
	changes := orphanDestroys(prior, map[string]struct{}{}, Options{Host: "h1"})
	if len(changes) != 1 || changes[0].Address != "debian_group.a" {
		t.Fatalf("got %+v, want only debian_group.a", changes)
	}
}

func TestDestroyEmitsCommands(t *testing.T) {
	cases := map[string]struct {
		p     provider
		prior state.ResourceState
		want  string
	}{
		"group":      {groupProvider{}, state.ResourceState{"type": "debian_group", "host": "h", "name": "deploy"}, "groupdel 'deploy'"},
		"user":       {userProvider{}, state.ResourceState{"type": "debian_user", "host": "h", "name": "app"}, "userdel 'app'"},
		"package":    {packageProvider{}, state.ResourceState{"type": "debian_package", "host": "h", "name": "nginx"}, "apt-get remove -y 'nginx'"},
		"directory":  {directoryProvider{}, state.ResourceState{"type": "debian_directory", "host": "h", "path": "/var/lib/app"}, "rm -rf -- '/var/lib/app'"},
		"apt_source": {aptSourceProvider{}, state.ResourceState{"type": "debian_apt_source", "host": "h", "path": "/etc/apt/sources.list.d/x.sources"}, "rm -f -- '/etc/apt/sources.list.d/x.sources'"},
		"apt_repository": {aptRepositoryProvider{}, state.ResourceState{
			"type": "debian_apt_repository", "host": "h", "path": "/etc/apt/sources.list.d/x.sources", "key_path": "/etc/apt/keyrings/x.asc",
		}, "rm -f -- '/etc/apt/keyrings/x.asc'"},
		"file":    {fileProvider{}, state.ResourceState{"type": "debian_file", "host": "h", "path": "/etc/motd"}, "rm -f -- '/etc/motd'"},
		"service": {serviceProvider{}, state.ResourceState{"type": "debian_service", "host": "h", "name": "nginx"}, "systemctl disable --now 'nginx'"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			runner := &fakeRunner{}
			e := &Engine{runner: runner}
			if err := tc.p.Destroy(context.Background(), e, tc.prior); err != nil {
				t.Fatal(err)
			}
			if len(runner.scripts) != 1 || !strings.Contains(runner.scripts[0], tc.want) {
				t.Fatalf("scripts = %q, want substring %q", runner.scripts, tc.want)
			}
		})
	}
}

func TestDestroyAuthorizedKeyFiltersLine(t *testing.T) {
	runner := &fakeRunner{}
	e := &Engine{runner: runner}
	prior := state.ResourceState{
		"type": "debian_authorized_key", "host": "h",
		"user": "deployer", "public_key": "ssh-ed25519 AAAAC3Nz deploy@ci",
	}
	if err := (authorizedKeyProvider{}).Destroy(context.Background(), e, prior); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"user='deployer'", "!($1==t && $2==b)", "/.ssh/authorized_keys"} {
		if !strings.Contains(runner.scripts[0], want) {
			t.Fatalf("script %q missing %q", runner.scripts[0], want)
		}
	}
}

func TestApplyChangeDispatchesDestroy(t *testing.T) {
	runner := &fakeRunner{}
	e := &Engine{runner: runner}
	ch := Change{
		Address: "debian_group.deploy", Action: "destroy",
		Prior: state.ResourceState{"type": "debian_group", "host": "h", "name": "deploy"},
	}
	if err := e.applyChange(context.Background(), ch); err != nil {
		t.Fatal(err)
	}
	if len(runner.scripts) != 1 || !strings.Contains(runner.scripts[0], "groupdel 'deploy'") {
		t.Fatalf("got %q", runner.scripts)
	}
}

func TestPlanResourceRejectsUnknownType(t *testing.T) {
	e := &Engine{runner: &fakeRunner{}}
	res := config.Resource{Type: "debian_bogus", Name: "x", Address: "debian_bogus.x", Host: "server1"}
	if _, err := e.planResource(context.Background(), res); err == nil {
		t.Fatal("expected an error for an unregistered resource type")
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
