package engine

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/mofelee/debianform/internal/core/ir"
)

const (
	sshControlPathMaxBytes = 103
	sshControlHashBytes    = 40
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

	controlMu     sync.Mutex
	controlDir    string
	controlConfig string
	controlErr    error

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
	sshPath := sshExecutable()
	cmd := exec.CommandContext(ctx, sshPath, args...)
	env, cleanup := nonInteractiveSSHEnv(os.Environ(), sshPath)
	defer cleanup()
	cmd.Env = env
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
	stderr := strings.TrimSpace(result.Stderr)
	hint := "check root SSH key/agent, SSH config ProxyCommand/ProxyJump, jump host access, and non-interactive login"
	if stderr == "" {
		return fmt.Errorf("ssh %s failed: %w; hint: %s", host, err, hint)
	}
	return fmt.Errorf("ssh %s failed: %w: %s\nhint: %s", host, err, stderr, hint)
}

func (r *SSHRunner) RunInput(ctx context.Context, host, remoteCommand string, input io.Reader) (Result, error) {
	args := append(r.SSHArgs(host), remoteCommand)
	return r.runSSH(ctx, host, args, input)
}

func (r *SSHRunner) RunCommand(ctx context.Context, host, remoteCommand string) (Result, error) {
	return r.Run(ctx, host, remoteCommand+"\n")
}

func (r *SSHRunner) SSHArgs(host string) []string {
	args, target := r.sshArgsAndTarget(host)
	return append(args, target)
}

func (r *SSHRunner) sshArgsAndTarget(host string) ([]string, string) {
	resolved := r.resolve(host)
	user := resolved.User
	if user == "" {
		user = "root"
	}
	args := []string{
		"-o", "BatchMode=yes",
		"-o", "NumberOfPasswordPrompts=0",
		"-o", "PasswordAuthentication=no",
		"-o", "KbdInteractiveAuthentication=no",
		"-o", "StrictHostKeyChecking=accept-new",
		"-l", user,
	}
	if config := r.sshConfigPath(); config != "" {
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
	return args, target
}

func (r *SSHRunner) Close(ctx context.Context) error {
	if r == nil {
		return nil
	}
	r.controlMu.Lock()
	dir := r.controlDir
	r.controlMu.Unlock()
	if dir == "" {
		return nil
	}

	var firstErr error
	for _, host := range r.closeHosts() {
		args, target := r.sshArgsAndTarget(host.Name)
		args = append(args, "-O", "exit", target)
		cmd := exec.CommandContext(ctx, "ssh", args...)
		if err := cmd.Run(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if err := os.RemoveAll(dir); err != nil && firstErr == nil {
		firstErr = err
	}
	return firstErr
}

func (r *SSHRunner) closeHosts() []Host {
	if r == nil || r.hosts == nil {
		return nil
	}
	hosts := make([]Host, 0, len(r.hosts))
	for _, host := range r.hosts {
		hosts = append(hosts, host)
	}
	return hosts
}

func (r *SSHRunner) sshConfigPath() string {
	if config := r.controlMasterConfig(); config != "" {
		return config
	}
	if config := os.Getenv("DBF_SSH_CONFIG"); config != "" {
		return config
	}
	return ""
}

func (r *SSHRunner) controlMasterConfig() string {
	if r == nil || os.Getenv("DBF_SSH_CONTROL_MASTER") == "0" {
		return ""
	}
	r.controlMu.Lock()
	defer r.controlMu.Unlock()
	if r.controlConfig != "" {
		return r.controlConfig
	}
	if r.controlErr != nil {
		return ""
	}
	dir, err := makeSSHControlDir()
	if err != nil {
		r.controlErr = err
		return ""
	}
	configPath := filepath.Join(dir, "config")
	controlPath := filepath.Join(dir, "%C")
	content := sshControlConfig(controlPath, os.Getenv("DBF_SSH_CONFIG"))
	if err := os.WriteFile(configPath, []byte(content), 0600); err != nil {
		_ = os.RemoveAll(dir)
		r.controlErr = err
		return ""
	}
	r.controlDir = dir
	r.controlConfig = configPath
	return configPath
}

func makeSSHControlDir() (string, error) {
	for _, root := range sshControlDirCandidates() {
		dir, err := os.MkdirTemp(root, "dbfssh-*")
		if err != nil {
			continue
		}
		if err := os.Chmod(dir, 0700); err != nil {
			_ = os.RemoveAll(dir)
			continue
		}
		if sshControlPathFits(filepath.Join(dir, "%C")) {
			return dir, nil
		}
		_ = os.RemoveAll(dir)
	}
	return "", fmt.Errorf("no usable short SSH ControlPath directory")
}

func sshControlDirCandidates() []string {
	var candidates []string
	if dir := os.Getenv("DBF_SSH_CONTROL_DIR"); dir != "" {
		candidates = append(candidates, dir)
		return candidates
	}
	candidates = append(candidates, "/tmp")
	if temp := os.TempDir(); temp != "" && temp != "/tmp" {
		candidates = append(candidates, temp)
	}
	return candidates
}

func sshControlPathFits(path string) bool {
	return sshControlPathExpandedLen(path) <= sshControlPathMaxBytes
}

func sshControlPathExpandedLen(path string) int {
	return len(strings.ReplaceAll(path, "%C", strings.Repeat("0", sshControlHashBytes)))
}

func sshControlConfig(controlPath, userConfig string) string {
	var b strings.Builder
	b.WriteString("Host *\n")
	b.WriteString("\tControlMaster auto\n")
	b.WriteString("\tControlPersist 10m\n")
	b.WriteString("\tControlPath ")
	b.WriteString(controlPath)
	b.WriteString("\n")
	b.WriteString("\tBatchMode yes\n")
	b.WriteString("\tNumberOfPasswordPrompts 0\n")
	b.WriteString("\tPasswordAuthentication no\n")
	b.WriteString("\tKbdInteractiveAuthentication no\n")
	if userConfig != "" {
		b.WriteString("Include ")
		b.WriteString(userConfig)
		b.WriteString("\n")
	} else {
		b.WriteString("Include ~/.ssh/config\n")
		b.WriteString("Include /etc/ssh/ssh_config\n")
	}
	return b.String()
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

func sshExecutable() string {
	path, err := exec.LookPath("ssh")
	if err != nil {
		return "ssh"
	}
	if filepath.IsAbs(path) {
		return path
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	return abs
}

func nonInteractiveSSHEnv(base []string, sshPath string) ([]string, func()) {
	env := setEnvValue(base, "SSH_ASKPASS", "/bin/false")
	env = setEnvValue(env, "SSH_ASKPASS_REQUIRE", "never")
	env = setEnvValue(env, "DISPLAY", "")

	dir, err := os.MkdirTemp("", "dbfssh-proxy-*")
	if err != nil {
		return env, func() {}
	}
	wrapper := filepath.Join(dir, "ssh")
	script := "#!/bin/sh\nexec " + shellQuote(sshPath) +
		" -o BatchMode=yes" +
		" -o NumberOfPasswordPrompts=0" +
		" -o PasswordAuthentication=no" +
		" -o KbdInteractiveAuthentication=no" +
		" \"$@\"\n"
	if err := os.WriteFile(wrapper, []byte(script), 0700); err != nil {
		_ = os.RemoveAll(dir)
		return env, func() {}
	}
	path := envValue(env, "PATH")
	if path == "" {
		path = dir
	} else {
		path = dir + string(os.PathListSeparator) + path
	}
	env = setEnvValue(env, "PATH", path)
	return env, func() { _ = os.RemoveAll(dir) }
}

func envValue(env []string, key string) string {
	prefix := key + "="
	for _, item := range env {
		if strings.HasPrefix(item, prefix) {
			return strings.TrimPrefix(item, prefix)
		}
	}
	return ""
}

func setEnvValue(env []string, key, value string) []string {
	prefix := key + "="
	out := make([]string, 0, len(env)+1)
	replaced := false
	for _, item := range env {
		if strings.HasPrefix(item, prefix) {
			if !replaced {
				out = append(out, prefix+value)
				replaced = true
			}
			continue
		}
		out = append(out, item)
	}
	if !replaced {
		out = append(out, prefix+value)
	}
	return out
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
