package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSSHRunnerExpandsHomeIdentityFile(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip(err)
	}
	runner := NewSSHRunner(map[string]Host{
		"server1": {
			Address:      "192.0.2.10",
			IdentityFile: "~/.ssh/id_ed25519",
		},
	})

	args := runner.SSHArgs("server1")
	want := filepath.Join(home, ".ssh", "id_ed25519")
	for _, arg := range args {
		if arg == want {
			return
		}
	}
	t.Fatalf("ssh args %q do not contain expanded identity file %q", strings.Join(args, " "), want)
}

func TestSSHRunnerIncludesConfiguredSSHConfig(t *testing.T) {
	t.Setenv("DBF_SSH_CONFIG", "/tmp/debianform-ssh-config")
	runner := NewSSHRunner(map[string]Host{
		"server1": {Address: "server1"},
	})

	args := runner.SSHArgs("server1")
	if len(args) < 2 || args[0] != "-F" || args[1] == "/tmp/debianform-ssh-config" {
		t.Fatalf("ssh args = %#v, want -F wrapper config prefix", args)
	}
	data, err := os.ReadFile(args[1])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "Include /tmp/debianform-ssh-config") {
		t.Fatalf("ssh wrapper config does not include DBF_SSH_CONFIG:\n%s", string(data))
	}
}
