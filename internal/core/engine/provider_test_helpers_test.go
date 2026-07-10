package engine

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"

	"github.com/mofelee/debianform/internal/core/graph"
)

type recordingRunner struct {
	outputs []Result
	errors  []error
	hosts   []string
	scripts []string
	inputs  []string
}

type countingLocalRunner struct {
	mu      sync.Mutex
	scripts []string
}

func (r *countingLocalRunner) Run(ctx context.Context, host, script string) (Result, error) {
	r.mu.Lock()
	r.scripts = append(r.scripts, script)
	r.mu.Unlock()
	cmd := exec.CommandContext(ctx, "sh", "-s")
	cmd.Stdin = strings.NewReader(script)
	var stdout strings.Builder
	var stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	result := Result{Stdout: stdout.String(), Stderr: stderr.String()}
	if err != nil {
		return result, fmt.Errorf("local shell failed: %w: %s", err, strings.TrimSpace(result.Stderr))
	}
	return result, nil
}

func (r *countingLocalRunner) RunInput(ctx context.Context, host, remoteCommand string, input io.Reader) (Result, error) {
	return Result{}, fmt.Errorf("unexpected RunInput")
}

func (r *countingLocalRunner) RunCommand(ctx context.Context, host, remoteCommand string) (Result, error) {
	return r.Run(ctx, host, remoteCommand+"\n")
}

func (r *recordingRunner) Run(ctx context.Context, host, script string) (Result, error) {
	r.hosts = append(r.hosts, host)
	r.scripts = append(r.scripts, script)
	return r.next()
}

func (r *recordingRunner) RunInput(ctx context.Context, host, remoteCommand string, input io.Reader) (Result, error) {
	data, err := io.ReadAll(input)
	if err != nil {
		return Result{}, err
	}
	r.hosts = append(r.hosts, host)
	r.scripts = append(r.scripts, remoteCommand)
	r.inputs = append(r.inputs, string(data))
	return r.next()
}

func (r *recordingRunner) next() (Result, error) {
	if len(r.outputs) == 0 {
		if len(r.errors) == 0 {
			return Result{}, nil
		}
		err := r.errors[0]
		r.errors = r.errors[1:]
		return Result{}, err
	}
	out := r.outputs[0]
	r.outputs = r.outputs[1:]
	if len(r.errors) == 0 {
		return out, nil
	}
	err := r.errors[0]
	r.errors = r.errors[1:]
	return out, err
}

func (r *recordingRunner) RunCommand(ctx context.Context, host, remoteCommand string) (Result, error) {
	return r.Run(ctx, host, remoteCommand)
}

func userGroupMembershipNode(user, group string) graph.Node {
	return graph.Node{
		Address: `host.server1.docker.user_group_membership["` + user + `:` + group + `"]`,
		Host:    "server1",
		Kind:    "user_group_membership",
		Desired: map[string]any{
			"user":   user,
			"group":  group,
			"ensure": "present",
			"note":   "user must log out and back in for docker group membership to affect existing sessions",
		},
	}
}

func dockerPackageConflictsNode(removeConflicts string) graph.Node {
	return graph.Node{
		Address: `host.docker1.docker.package_conflicts`,
		Host:    "docker1",
		Kind:    "docker_package_conflicts",
		Desired: map[string]any{
			"packages":         []string{"docker.io", "docker-doc", "docker-compose", "podman-docker", "containerd", "runc"},
			"remove_conflicts": removeConflicts,
			"ensure":           "absent",
		},
	}
}

func dockerComposeProjectNode(state string) graph.Node {
	return graph.Node{
		Address: `host.compose1.docker.compose["app"].project`,
		Host:    "compose1",
		Kind:    "docker_compose_project",
		Desired: map[string]any{
			"directory":      "/opt/app",
			"project":        "app",
			"files":          []string{"/opt/app/compose.yaml"},
			"env_files":      []string{"/opt/app/.env"},
			"state":          state,
			"pull":           "missing",
			"recreate":       "auto",
			"remove_orphans": false,
		},
	}
}

func writeOnlyFileNode(version string) graph.Node {
	desired := map[string]any{
		"path":               "/etc/app/token",
		"owner":              "root",
		"group":              "root",
		"mode":               "0600",
		"ensure":             "present",
		"sensitive":          true,
		"content_write_only": true,
		"content_version":    version,
	}
	payload := cloneMap(desired)
	payload["content"] = "not-a-real-ephemeral-token"
	return graph.Node{
		Address:         "host.server1.files.file[\"/etc/app/token\"]",
		Host:            "server1",
		Kind:            "file",
		Desired:         desired,
		ProviderPayload: payload,
	}
}

func anyMapText(values map[string]any) string {
	parts := make([]string, 0, len(values))
	for key, value := range values {
		parts = append(parts, key+"="+fmt.Sprint(value))
	}
	return strings.Join(parts, "\n")
}
