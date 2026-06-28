package engine

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"

	"github.com/mofelee/debianform/internal/core/ir"
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

	initialAuthMu        sync.Mutex
	initialAuthRunning   bool
	initialAuthSucceeded bool
	initialAuthErr       error
	initialAuthWait      chan struct{}
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
	return r.runSSH(ctx, host, args, strings.NewReader(script))
}

func (r *SSHRunner) runSSH(ctx context.Context, host string, args []string, input io.Reader) (Result, error) {
	release, err := r.acquireInitialAuthSlot(ctx)
	if err != nil {
		return Result{}, err
	}
	cmd := exec.CommandContext(ctx, "ssh", args...)
	cmd.Stdin = input
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	rawErr := cmd.Run()
	result := Result{Stdout: stdout.String(), Stderr: stderr.String()}
	err = sshRunError(host, result, rawErr)
	release(sshCommandReachedRemote(rawErr), err)
	return result, err
}

func (r *SSHRunner) acquireInitialAuthSlot(ctx context.Context) (func(bool, error), error) {
	if r == nil {
		return func(bool, error) {}, nil
	}
	for {
		r.initialAuthMu.Lock()
		if r.initialAuthSucceeded {
			r.initialAuthMu.Unlock()
			return func(bool, error) {}, nil
		}
		if r.initialAuthErr != nil {
			err := r.initialAuthErr
			r.initialAuthMu.Unlock()
			return nil, err
		}
		if !r.initialAuthRunning {
			r.initialAuthRunning = true
			if r.initialAuthWait == nil {
				r.initialAuthWait = make(chan struct{})
			}
			r.initialAuthMu.Unlock()
			return r.releaseInitialAuthSlot, nil
		}
		wait := r.initialAuthWait
		if wait == nil {
			wait = make(chan struct{})
			r.initialAuthWait = wait
		}
		r.initialAuthMu.Unlock()

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-wait:
		}
	}
}

func (r *SSHRunner) releaseInitialAuthSlot(reachedRemote bool, runErr error) {
	r.initialAuthMu.Lock()
	if reachedRemote {
		r.initialAuthSucceeded = true
	} else if runErr != nil {
		r.initialAuthErr = fmt.Errorf("initial ssh connection failed: %w", runErr)
	}
	r.initialAuthRunning = false
	wait := r.initialAuthWait
	r.initialAuthWait = make(chan struct{})
	if wait != nil {
		close(wait)
	}
	r.initialAuthMu.Unlock()
}

func sshCommandReachedRemote(err error) bool {
	if err == nil {
		return true
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		code := exitErr.ExitCode()
		return code >= 0 && code != 255
	}
	return false
}

func sshRunError(host string, result Result, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("ssh %s failed: %w: %s", host, err, strings.TrimSpace(result.Stderr))
}

func (r *SSHRunner) RunInput(ctx context.Context, host, remoteCommand string, input io.Reader) (Result, error) {
	args := append(r.SSHArgs(host), remoteCommand)
	return r.runSSH(ctx, host, args, input)
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
