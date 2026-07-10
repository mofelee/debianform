package engine

import (
	"context"
	"strings"
	"testing"

	corestate "github.com/mofelee/debianform/internal/core/state"
)

func TestNativeProviderUserGroupMembershipPlanAndApply(t *testing.T) {
	runner := &recordingRunner{outputs: []Result{{Stdout: "deploy:x:1000:1000::/home/deploy:/bin/bash\ndeploy\ndeploy\n"}}}
	provider := NewNativeProvider(runner)
	node := userGroupMembershipNode("deploy", "docker")

	got, err := provider.Plan(context.Background(), node, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Action != ActionUpdate || !strings.Contains(got.Summary, "log out and back in") {
		t.Fatalf("membership plan = %#v, want update with relogin hint", got)
	}
	if _, err := provider.Apply(context.Background(), Step{Node: node, Action: ActionUpdate}); err != nil {
		t.Fatal(err)
	}
	applied := runner.scripts[len(runner.scripts)-1]
	for _, want := range []string{
		"getent passwd 'deploy'",
		"getent group 'docker'",
		"usermod -aG 'docker' 'deploy'",
		"must log out and back in",
	} {
		if !strings.Contains(applied, want) {
			t.Fatalf("membership apply script missing %q:\n%s", want, applied)
		}
	}
}

func TestNativeProviderUserGroupMembershipPlanNoopWhenAlreadyPresent(t *testing.T) {
	runner := &recordingRunner{outputs: []Result{{Stdout: "deploy:x:1000:1000::/home/deploy:/bin/bash\ndeploy\ndeploy docker\n"}}}
	provider := NewNativeProvider(runner)
	node := userGroupMembershipNode("deploy", "docker")
	prior := &corestate.Resource{Ownership: "managed", DesiredDigest: corestate.DesiredDigest(node.Desired)}

	got, err := provider.Plan(context.Background(), node, prior)
	if err != nil {
		t.Fatal(err)
	}
	if got.Action != ActionNoOp {
		t.Fatalf("membership plan action = %q, want no-op; observed=%#v", got.Action, got.Observed)
	}
}

func TestNativeProviderUserGroupMembershipPlanErrorsWhenUserMissing(t *testing.T) {
	runner := &recordingRunner{outputs: []Result{{Stdout: "__ABSENT__\n"}}}
	provider := NewNativeProvider(runner)
	node := userGroupMembershipNode("deploy", "docker")

	_, err := provider.Plan(context.Background(), node, nil)
	if err == nil || !strings.Contains(err.Error(), `user "deploy" does not exist`) || !strings.Contains(err.Error(), `users.user["deploy"]`) {
		t.Fatalf("membership missing user error = %v", err)
	}
}

func TestNativeProviderUserGroupMembershipPlansCreateWhenManagedUserDependencyIsMissing(t *testing.T) {
	runner := &recordingRunner{outputs: []Result{{Stdout: "__ABSENT__\n"}}}
	provider := NewNativeProvider(runner)
	node := userGroupMembershipNode("deploy", "docker")
	node.DependsOn = []string{`host.server1.users.user["deploy"]`, `host.server1.docker.group["docker"]`}

	got, err := provider.Plan(context.Background(), node, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Action != ActionCreate {
		t.Fatalf("membership missing managed user action = %q, want create", got.Action)
	}
}

func TestNativeProviderUserGroupMembershipDestroyRemovesSupplementaryOnly(t *testing.T) {
	runner := &recordingRunner{}
	provider := NewNativeProvider(runner)
	node := userGroupMembershipNode("deploy", "docker")
	prior := &corestate.Resource{
		Host:    node.Host,
		Kind:    node.Kind,
		Desired: cloneMap(node.Desired),
	}

	if err := provider.Destroy(context.Background(), Step{Address: node.Address, Host: node.Host, Prior: prior}); err != nil {
		t.Fatal(err)
	}
	if len(runner.scripts) != 1 {
		t.Fatalf("scripts = %#v, want destroy command", runner.scripts)
	}
	for _, want := range []string{
		"gpasswd -d 'deploy' 'docker'",
		`[ "$(id -gn 'deploy')" != 'docker' ]`,
	} {
		if !strings.Contains(runner.scripts[0], want) {
			t.Fatalf("membership destroy script missing %q:\n%s", want, runner.scripts[0])
		}
	}
}
