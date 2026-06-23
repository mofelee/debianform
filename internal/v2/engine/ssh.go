package engine

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/mofelee/debianform/internal/v2/ir"
)

type Host struct {
	Name         string
	Address      string
	Port         int
	User         string
	IdentityFile string
}

type Result struct {
	Stdout string
	Stderr string
}

type Runner interface {
	Run(ctx context.Context, host, script string) (Result, error)
	RunInput(ctx context.Context, host, remoteCommand string, input io.Reader) (Result, error)
	RunCommand(ctx context.Context, host, remoteCommand string) (Result, error)
}

type SSHRunner struct {
	hosts map[string]Host
}

func NewSSHRunner(hosts map[string]Host) *SSHRunner {
	return &SSHRunner{hosts: hosts}
}

func HostsFromProgram(program *ir.Program) map[string]Host {
	hosts := map[string]Host{}
	if program == nil {
		return hosts
	}
	for _, host := range program.Hosts {
		hosts[host.Name] = Host{
			Name:         host.Name,
			Address:      host.SSH.Host,
			Port:         host.SSH.Port,
			User:         host.SSH.User,
			IdentityFile: host.SSH.IdentityFile,
		}
	}
	return hosts
}

func (r *SSHRunner) Run(ctx context.Context, host, script string) (Result, error) {
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

func (r *SSHRunner) RunInput(ctx context.Context, host, remoteCommand string, input io.Reader) (Result, error) {
	args := append(r.SSHArgs(host), remoteCommand)
	cmd := exec.CommandContext(ctx, "ssh", args...)
	cmd.Stdin = input
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

func (r *SSHRunner) RunCommand(ctx context.Context, host, remoteCommand string) (Result, error) {
	return r.Run(ctx, host, remoteCommand+"\n")
}

func (r *SSHRunner) SSHArgs(host string) []string {
	resolved := r.resolve(host)
	user := resolved.User
	if user == "" {
		user = "root"
	}
	args := []string{
		"-o", "BatchMode=yes",
		"-o", "StrictHostKeyChecking=accept-new",
		"-l", user,
	}
	if config := os.Getenv("DBF_SSH_CONFIG"); config != "" {
		args = append([]string{"-F", config}, args...)
	}
	if resolved.Port != 0 && resolved.Port != 22 {
		args = append(args, "-p", strconv.Itoa(resolved.Port))
	}
	if resolved.IdentityFile != "" {
		args = append(args, "-i", expandHome(resolved.IdentityFile))
	}
	target := host
	if resolved.Address != "" {
		target = resolved.Address
	}
	args = append(args, target)
	return args
}

func (r *SSHRunner) resolve(host string) Host {
	if r == nil || r.hosts == nil {
		return Host{Name: host, Address: host, User: "root"}
	}
	if resolved, ok := r.hosts[host]; ok {
		return resolved
	}
	return Host{Name: host, Address: host, User: "root"}
}

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

func expandHome(path string) string {
	if path == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
		return path
	}
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return home + "/" + strings.TrimPrefix(path, "~/")
		}
		return path
	}
	return path
}
