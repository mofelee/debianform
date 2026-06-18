package sshx

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/mofelee/debianform/internal/config"
)

type Runner struct {
	hosts map[string]config.Host
}

type Result struct {
	Stdout string
	Stderr string
}

func NewRunner(hosts map[string]config.Host) *Runner {
	return &Runner{hosts: hosts}
}

func (r *Runner) Run(ctx context.Context, host string, script string) (Result, error) {
	args := append(r.SSHArgs(host), "sh", "-s")
	cmd := exec.CommandContext(ctx, "ssh", args...)
	cmd.Stdin = strings.NewReader(script)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	result := Result{Stdout: stdout.String(), Stderr: stderr.String()}
	if err != nil {
		return result, fmt.Errorf("ssh %s failed: %w: %s", host, err, strings.TrimSpace(result.Stderr))
	}
	return result, nil
}

func (r *Runner) RunCommand(ctx context.Context, host string, remoteCommand string) (Result, error) {
	return r.Run(ctx, host, remoteCommand+"\n")
}

func (r *Runner) SSHArgs(host string) []string {
	resolved := r.resolve(host)
	args := []string{
		"-o", "BatchMode=yes",
		"-o", "StrictHostKeyChecking=accept-new",
		"-l", "root",
	}
	if resolved.Port != "" {
		args = append(args, "-p", resolved.Port)
	}
	if resolved.IdentityFile != "" {
		args = append(args, "-i", expandHome(resolved.IdentityFile))
	}
	target := host
	if resolved.SSHConfigHost != "" {
		target = resolved.SSHConfigHost
	} else if resolved.Address != "" {
		target = resolved.Address
	}
	args = append(args, target)
	return args
}

func (r *Runner) resolve(host string) config.Host {
	if r == nil || r.hosts == nil {
		return config.Host{Name: host}
	}
	if resolved, ok := r.hosts[host]; ok {
		return resolved
	}
	return config.Host{Name: host}
}

func ShellQuote(s string) string {
	if s == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

func expandHome(path string) string {
	if path == "~" {
		return "$HOME"
	}
	if strings.HasPrefix(path, "~/") {
		return "$HOME/" + strings.TrimPrefix(path, "~/")
	}
	return path
}
