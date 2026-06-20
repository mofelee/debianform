package engine

import (
	"context"
	"strings"

	"github.com/mofelee/debianform/internal/v1/config"
	"github.com/mofelee/debianform/internal/v1/sshx"
	"github.com/mofelee/debianform/internal/v1/state"
)

// groupProvider manages a Unix group via getent/groupadd/groupmod/groupdel.
type groupProvider struct{}

func (groupProvider) Desired(res config.Resource) (Desired, error) {
	d := baseDesired(res)
	d.Name = stringAttr(res, "name", d.Name)
	d.Ensure = stringAttr(res, "ensure", "present")
	d.GID = stringAttr(res, "gid", "")
	d.System = boolAttr(res, "system", false)
	return d, nil
}

func (groupProvider) Plan(ctx context.Context, e *Engine, res config.Resource, d Desired) (Change, error) {
	exists, currentGID, err := e.readGroup(ctx, res.Host, d.Name)
	if err != nil {
		return Change{}, err
	}

	if d.Ensure == "absent" {
		if exists {
			return change(res, d, "delete", "remove group "+d.Name), nil
		}
		return noChange(res, d), nil
	}

	if !exists {
		summary := "create group " + d.Name
		if d.GID != "" {
			summary += " with gid " + d.GID
		}
		return change(res, d, "create", summary), nil
	}

	if d.GID != "" && currentGID != d.GID {
		return change(res, d, "update", "set group "+d.Name+" gid to "+d.GID), nil
	}
	return noChange(res, d), nil
}

func (groupProvider) Apply(ctx context.Context, e *Engine, change Change) error {
	d := change.Desired
	name := sshx.ShellQuote(d.Name)

	lines := []string{"set -eu"}
	if d.Ensure == "absent" {
		lines = append(lines, "if getent group "+name+" >/dev/null; then groupdel "+name+"; fi")
		_, err := e.runner.Run(ctx, change.Resource.Host, strings.Join(lines, "\n")+"\n")
		return err
	}

	add := "groupadd"
	if d.System {
		add += " -r"
	}
	if d.GID != "" {
		add += " -g " + sshx.ShellQuote(d.GID)
	}
	add += " " + name

	mod := ":"
	if d.GID != "" {
		mod = "groupmod -g " + sshx.ShellQuote(d.GID) + " " + name
	}

	lines = append(lines, "if getent group "+name+" >/dev/null; then "+mod+"; else "+add+"; fi")
	_, err := e.runner.Run(ctx, change.Resource.Host, strings.Join(lines, "\n")+"\n")
	return err
}

func (groupProvider) Destroy(ctx context.Context, e *Engine, prior state.ResourceState) error {
	name := priorString(prior, "name")
	if name == "" {
		return nil
	}
	script := "if getent group " + sshx.ShellQuote(name) + " >/dev/null; then groupdel " + sshx.ShellQuote(name) + "; fi\n"
	_, err := e.runner.Run(ctx, priorString(prior, "host"), script)
	return err
}

// readGroup returns whether the group exists and its current gid, parsed from
// a getent group line of the form "name:x:gid:members".
func (e *Engine) readGroup(ctx context.Context, host, name string) (exists bool, gid string, err error) {
	script := "getent group " + sshx.ShellQuote(name) + " || true\n"
	result, err := e.runner.Run(ctx, host, script)
	if err != nil {
		return false, "", err
	}
	line := strings.TrimSpace(result.Stdout)
	if line == "" {
		return false, "", nil
	}
	fields := strings.Split(line, ":")
	if len(fields) >= 3 {
		gid = fields[2]
	}
	return true, gid, nil
}
