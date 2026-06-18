package engine

import (
	"context"
	"sort"
	"strings"

	"github.com/mofelee/debianform/internal/config"
	"github.com/mofelee/debianform/internal/sshx"
)

// userProvider manages a Unix user via getent/useradd/usermod/userdel.
type userProvider struct{}

func (userProvider) Desired(res config.Resource) (Desired, error) {
	d := baseDesired(res)
	d.Name = stringAttr(res, "name", d.Name)
	d.Ensure = stringAttr(res, "ensure", "present")
	d.UID = stringAttr(res, "uid", "")
	d.GID = stringAttr(res, "gid", "")
	d.Home = stringAttr(res, "home", "")
	d.Shell = stringAttr(res, "shell", "")
	d.System = boolAttr(res, "system", false)
	if value, ok := res.Attrs["groups"]; ok {
		d.Groups = config.StringList(value)
	}
	return d, nil
}

func (userProvider) Plan(ctx context.Context, e *Engine, res config.Resource, d Desired) (Change, error) {
	cur, err := e.readUser(ctx, res.Host, d.Name)
	if err != nil {
		return Change{}, err
	}

	if d.Ensure == "absent" {
		if cur.exists {
			return change(res, d, "delete", "remove user "+d.Name), nil
		}
		return noChange(res, d), nil
	}

	if !cur.exists {
		return change(res, d, "create", "create user "+d.Name), nil
	}

	var reasons []string
	if d.UID != "" && cur.uid != d.UID {
		reasons = append(reasons, "uid")
	}
	if d.GID != "" && d.GID != cur.gidNum && d.GID != cur.primaryGroup {
		reasons = append(reasons, "primary group")
	}
	if d.Home != "" && cur.home != d.Home {
		reasons = append(reasons, "home")
	}
	if d.Shell != "" && cur.shell != d.Shell {
		reasons = append(reasons, "shell")
	}
	if len(d.Groups) > 0 && !sameStringSet(d.Groups, cur.groups) {
		reasons = append(reasons, "groups")
	}
	if len(reasons) > 0 {
		return change(res, d, "update", "update user "+d.Name+" ("+strings.Join(reasons, ", ")+")"), nil
	}
	return noChange(res, d), nil
}

func (userProvider) Apply(ctx context.Context, e *Engine, change Change) error {
	d := change.Desired
	name := sshx.ShellQuote(d.Name)

	lines := []string{"set -eu"}
	if d.Ensure == "absent" {
		lines = append(lines, "if getent passwd "+name+" >/dev/null; then userdel "+name+"; fi")
		_, err := e.runner.Run(ctx, change.Resource.Host, strings.Join(lines, "\n")+"\n")
		return err
	}

	flags := userFlags(d)

	add := "useradd"
	if d.System {
		add += " -r"
	} else {
		add += " -m"
	}
	add += flags + " " + name

	mod := ":"
	if flags != "" {
		mod = "usermod" + flags + " " + name
	}

	lines = append(lines, "if getent passwd "+name+" >/dev/null; then "+mod+"; else "+add+"; fi")
	_, err := e.runner.Run(ctx, change.Resource.Host, strings.Join(lines, "\n")+"\n")
	return err
}

// userFlags builds the shared useradd/usermod option string for the managed
// attributes. Unset attributes are left untouched.
func userFlags(d Desired) string {
	var flags string
	if d.UID != "" {
		flags += " -u " + sshx.ShellQuote(d.UID)
	}
	if d.GID != "" {
		flags += " -g " + sshx.ShellQuote(d.GID)
	}
	if len(d.Groups) > 0 {
		flags += " -G " + sshx.ShellQuote(strings.Join(d.Groups, ","))
	}
	if d.Home != "" {
		flags += " -d " + sshx.ShellQuote(d.Home)
	}
	if d.Shell != "" {
		flags += " -s " + sshx.ShellQuote(d.Shell)
	}
	return flags
}

type userState struct {
	exists       bool
	uid          string
	gidNum       string
	primaryGroup string
	home         string
	shell        string
	groups       []string // supplementary groups (primary excluded)
}

// readUser reads a user's identity from getent passwd plus its primary and
// supplementary group names from id.
func (e *Engine) readUser(ctx context.Context, host, name string) (userState, error) {
	quoted := sshx.ShellQuote(name)
	script := "if getent passwd " + quoted + " >/dev/null 2>&1; then\n" +
		"  getent passwd " + quoted + "\n" +
		"  id -gn " + quoted + "\n" +
		"  id -nG " + quoted + "\n" +
		"else\n" +
		"  echo __ABSENT__\n" +
		"fi\n"
	result, err := e.runner.Run(ctx, host, script)
	if err != nil {
		return userState{}, err
	}

	lines := strings.Split(strings.TrimRight(result.Stdout, "\n"), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) == "__ABSENT__" || strings.TrimSpace(lines[0]) == "" {
		return userState{}, nil
	}

	st := userState{exists: true}
	fields := strings.Split(lines[0], ":")
	if len(fields) >= 7 {
		st.uid = fields[2]
		st.gidNum = fields[3]
		st.home = fields[5]
		st.shell = fields[6]
	}
	if len(lines) > 1 {
		st.primaryGroup = strings.TrimSpace(lines[1])
	}
	if len(lines) > 2 {
		for _, g := range strings.Fields(lines[2]) {
			if g != st.primaryGroup {
				st.groups = append(st.groups, g)
			}
		}
	}
	return st, nil
}

func sameStringSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	as := append([]string(nil), a...)
	bs := append([]string(nil), b...)
	sort.Strings(as)
	sort.Strings(bs)
	for i := range as {
		if as[i] != bs[i] {
			return false
		}
	}
	return true
}
