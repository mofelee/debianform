package engine

import (
	"context"
	"fmt"
	"strings"

	"github.com/mofelee/debianform/internal/core/graph"
	corestate "github.com/mofelee/debianform/internal/core/state"
)

func (p NativeProvider) planGroup(ctx context.Context, node graph.Node, prior *corestate.Resource) (ProviderPlan, error) {
	name := stringDesired(node, "name")
	result, err := p.Runner.Run(ctx, node.Host, "getent group "+shellQuote(name)+" || true\n")
	if err != nil {
		return ProviderPlan{}, err
	}
	line := strings.TrimSpace(result.Stdout)
	exists := line != ""
	gid := ""
	if fields := strings.Split(line, ":"); len(fields) >= 3 {
		gid = fields[2]
	}
	observed := map[string]any{"exists": exists, "gid": gid}
	if ensureAbsent(node) {
		if exists {
			return ProviderPlan{Action: ActionDelete, Summary: "remove group " + name, Observed: observed, Ownership: ownership(prior)}, nil
		}
		return absentInSyncPlan(prior, "already absent group "+name, observed), nil
	}
	wantGID := stringDesired(node, "gid")
	if !exists {
		return ProviderPlan{Action: ActionCreate, Summary: "create group " + name, Observed: observed, Ownership: ownership(prior)}, nil
	}
	if wantGID != "" && gid != wantGID {
		return ProviderPlan{Action: ActionUpdate, Summary: "set group " + name + " gid to " + wantGID, Observed: observed, Ownership: ownership(prior)}, nil
	}
	return inSyncPlan(node, prior, "no changes for group "+name, observed), nil
}

func (p NativeProvider) applyGroup(ctx context.Context, step Step) (map[string]any, error) {
	name := stringDesired(step.Node, "name")
	quoted := shellQuote(name)
	lines := []string{"set -eu"}
	if ensureAbsent(step.Node) || step.Action == ActionDelete {
		lines = append(lines, "if getent group "+quoted+" >/dev/null; then groupdel "+quoted+"; fi")
	} else {
		add := "groupadd"
		if boolDesired(step.Node, "system") {
			add += " -r"
		}
		if gid := stringDesired(step.Node, "gid"); gid != "" {
			add += " -g " + shellQuote(gid)
		}
		add += " " + quoted
		mod := ":"
		if gid := stringDesired(step.Node, "gid"); gid != "" {
			mod = "groupmod -g " + shellQuote(gid) + " " + quoted
		}
		lines = append(lines, "if getent group "+quoted+" >/dev/null; then "+mod+"; else "+add+"; fi")
	}
	_, err := p.Runner.Run(ctx, step.Node.Host, strings.Join(lines, "\n")+"\n")
	if err != nil {
		return nil, err
	}
	return map[string]any{"exists": !ensureAbsent(step.Node), "desired_digest": corestate.DesiredDigest(step.Node.Desired)}, nil
}

func (p NativeProvider) planUser(ctx context.Context, node graph.Node, prior *corestate.Resource) (ProviderPlan, error) {
	name := stringDesired(node, "name")
	cur, err := p.readUser(ctx, node.Host, name)
	if err != nil {
		return ProviderPlan{}, err
	}
	observed := map[string]any{"exists": cur.exists, "uid": cur.uid, "group": cur.primaryGroup, "home": cur.home, "shell": cur.shell, "groups": cur.groups}
	if ensureAbsent(node) {
		if cur.exists {
			return ProviderPlan{Action: ActionDelete, Summary: "remove user " + name, Observed: observed, Ownership: ownership(prior)}, nil
		}
		return absentInSyncPlan(prior, "already absent user "+name, observed), nil
	}
	if !cur.exists {
		return ProviderPlan{Action: ActionCreate, Summary: "create user " + name, Observed: observed, Ownership: ownership(prior)}, nil
	}
	var reasons []string
	if want := stringDesired(node, "uid"); want != "" && cur.uid != want {
		reasons = append(reasons, "uid")
	}
	if want := stringDesired(node, "group"); want != "" && want != cur.gidNum && want != cur.primaryGroup {
		reasons = append(reasons, "primary group")
	}
	if want := stringDesired(node, "home"); want != "" && cur.home != want {
		reasons = append(reasons, "home")
	}
	if want := stringDesired(node, "shell"); want != "" && cur.shell != want {
		reasons = append(reasons, "shell")
	}
	if want := stringListDesired(node, "groups"); len(want) > 0 && !sameStringSet(want, cur.groups) {
		reasons = append(reasons, "groups")
	}
	if len(reasons) > 0 {
		return ProviderPlan{Action: ActionUpdate, Summary: "update user " + name + " (" + strings.Join(reasons, ", ") + ")", Observed: observed, Ownership: ownership(prior)}, nil
	}
	return inSyncPlan(node, prior, "no changes for user "+name, observed), nil
}

func (p NativeProvider) applyUser(ctx context.Context, step Step) (map[string]any, error) {
	name := stringDesired(step.Node, "name")
	quoted := shellQuote(name)
	lines := []string{"set -eu"}
	if ensureAbsent(step.Node) || step.Action == ActionDelete {
		lines = append(lines, "if getent passwd "+quoted+" >/dev/null; then userdel "+quoted+"; fi")
	} else {
		flags := userFlags(step.Node)
		add := "useradd"
		if boolDesired(step.Node, "system") {
			add += " -r"
		} else {
			add += " -m"
		}
		add += flags + " " + quoted
		mod := ":"
		if flags != "" {
			mod = "usermod" + flags + " " + quoted
		}
		lines = append(lines, "if getent passwd "+quoted+" >/dev/null; then "+mod+"; else "+add+"; fi")
	}
	_, err := p.Runner.Run(ctx, step.Node.Host, strings.Join(lines, "\n")+"\n")
	if err != nil {
		return nil, err
	}
	return map[string]any{"exists": !ensureAbsent(step.Node), "desired_digest": corestate.DesiredDigest(step.Node.Desired)}, nil
}

func (p NativeProvider) planUserGroupMembership(ctx context.Context, node graph.Node, prior *corestate.Resource) (ProviderPlan, error) {
	user := stringDesired(node, "user")
	group := stringDesired(node, "group")
	cur, err := p.readUser(ctx, node.Host, user)
	if err != nil {
		return ProviderPlan{}, err
	}
	observed := map[string]any{
		"exists":        cur.exists,
		"user":          user,
		"group":         group,
		"primary_group": cur.primaryGroup,
		"present":       cur.exists && userInGroup(cur, group),
		"groups":        cur.groups,
	}
	if ensureAbsent(node) {
		if cur.exists && stringSliceContains(cur.groups, group) {
			return ProviderPlan{Action: ActionDelete, Summary: "remove user " + user + " from group " + group, Observed: observed, Ownership: ownership(prior)}, nil
		}
		return absentInSyncPlan(prior, "already absent user group membership "+user+":"+group, observed), nil
	}
	if !cur.exists {
		if membershipDependsOnManagedUser(node, user) {
			return ProviderPlan{Action: ActionCreate, Summary: "add user " + user + " to group " + group + " after creating user", Observed: observed, Ownership: ownership(prior)}, nil
		}
		return ProviderPlan{}, fmt.Errorf("%s: user %q does not exist; declare users.user[%q] or create it before applying group membership %q", node.Address, user, user, group)
	}
	if userInGroup(cur, group) {
		return inSyncPlan(node, prior, "no changes for user group membership "+user+":"+group, observed), nil
	}
	return ProviderPlan{
		Action:    ActionUpdate,
		Summary:   "add user " + user + " to group " + group + " (log out and back in for group session to refresh)",
		Observed:  observed,
		Ownership: ownership(prior),
	}, nil
}

func membershipDependsOnManagedUser(node graph.Node, user string) bool {
	needle := `.users.user["` + user + `"]`
	for _, dep := range node.DependsOn {
		if strings.Contains(dep, needle) {
			return true
		}
	}
	return false
}

func (p NativeProvider) applyUserGroupMembership(ctx context.Context, step Step) (map[string]any, error) {
	user := stringDesired(step.Node, "user")
	group := stringDesired(step.Node, "group")
	lines := []string{"set -eu"}
	if ensureAbsent(step.Node) || step.Action == ActionDelete {
		lines = append(lines, "if getent passwd "+shellQuote(user)+" >/dev/null && [ \"$(id -gn "+shellQuote(user)+")\" != "+shellQuote(group)+" ] && id -nG "+shellQuote(user)+" | tr ' ' '\\n' | grep -Fx "+shellQuote(group)+" >/dev/null; then gpasswd -d "+shellQuote(user)+" "+shellQuote(group)+"; fi")
	} else {
		lines = append(lines,
			"getent passwd "+shellQuote(user)+" >/dev/null || { echo "+shellQuote("debianform: user "+user+" does not exist; create it before applying group membership "+group)+" >&2; exit 1; }",
			"getent group "+shellQuote(group)+" >/dev/null || { echo "+shellQuote("debianform: group "+group+" does not exist; create it before applying membership for user "+user)+" >&2; exit 1; }",
			"usermod -aG "+shellQuote(group)+" "+shellQuote(user),
			"echo "+shellQuote("debianform: user "+user+" must log out and back in for "+group+" group membership to affect existing sessions"),
		)
	}
	_, err := p.Runner.Run(ctx, step.Node.Host, strings.Join(lines, "\n")+"\n")
	if err != nil {
		return nil, err
	}
	if ensureAbsent(step.Node) || step.Action == ActionDelete {
		return map[string]any{"exists": true, "present": false, "desired_digest": corestate.DesiredDigest(step.Node.Desired)}, nil
	}
	return map[string]any{"exists": true, "present": true, "desired_digest": corestate.DesiredDigest(step.Node.Desired)}, nil
}

func (p NativeProvider) planAuthorizedKey(ctx context.Context, node graph.Node, prior *corestate.Resource) (ProviderPlan, error) {
	keytype, keyblob, err := splitAuthorizedKey(stringDesired(node, "key"))
	if err != nil {
		return ProviderPlan{}, fmt.Errorf("%s %w", node.Address, err)
	}
	script := authorizedKeyPreamble(node.Desired) +
		"if [ -n \"$home\" ] && [ -f \"$file\" ] && awk -v t=" + shellQuote(keytype) +
		" -v b=" + shellQuote(keyblob) +
		" '($1==t && $2==b){f=1} END{exit f?0:1}' \"$file\"; then echo present; else echo absent; fi\n"
	result, err := p.Runner.Run(ctx, node.Host, script)
	if err != nil {
		return ProviderPlan{}, err
	}
	present := strings.Contains(result.Stdout, "present")
	observed := map[string]any{"present": present}
	if ensureAbsent(node) {
		if present {
			return ProviderPlan{Action: ActionDelete, Summary: "remove authorized key for " + stringDesired(node, "user"), Observed: observed, Ownership: ownership(prior)}, nil
		}
		return absentInSyncPlan(prior, "already absent authorized key", observed), nil
	}
	if !present {
		return ProviderPlan{Action: ActionCreate, Summary: "add authorized key for " + stringDesired(node, "user"), Observed: observed, Ownership: ownership(prior)}, nil
	}
	return inSyncPlan(node, prior, "no changes for authorized key", observed), nil
}

func (p NativeProvider) applyAuthorizedKey(ctx context.Context, step Step) (map[string]any, error) {
	key := stringDesired(step.Node, "key")
	keytype, keyblob, err := splitAuthorizedKey(key)
	if err != nil {
		return nil, err
	}
	match := "awk -v t=" + shellQuote(keytype) + " -v b=" + shellQuote(keyblob)
	var body string
	if ensureAbsent(step.Node) || step.Action == ActionDelete {
		body = "if [ -z \"$home\" ]; then exit 0; fi\n" +
			"if [ -f \"$file\" ]; then\n" +
			"  tmp=$(mktemp)\n" +
			"  " + match + " '!($1==t && $2==b)' \"$file\" > \"$tmp\"\n" +
			"  cat \"$tmp\" > \"$file\"\n" +
			"  rm -f \"$tmp\"\n" +
			"fi\n"
	} else {
		body = "dir=$(dirname \"$file\")\n" +
			"group=$(id -gn \"$user\")\n" +
			"mkdir -p \"$dir\"\n" +
			"chmod 0700 \"$dir\"\n" +
			"if ! { [ -f \"$file\" ] && " + match + " '($1==t && $2==b){f=1} END{exit f?0:1}' \"$file\"; }; then\n" +
			"  printf '%s\\n' " + shellQuote(key) + " >> \"$file\"\n" +
			"fi\n" +
			"chmod 0600 \"$file\"\n" +
			"chown \"$user\":\"$group\" \"$dir\" \"$file\"\n"
	}
	_, err = p.Runner.Run(ctx, step.Node.Host, "set -eu\n"+authorizedKeyPreamble(step.Node.Desired)+body)
	if err != nil {
		return nil, err
	}
	return map[string]any{"present": !ensureAbsent(step.Node), "desired_digest": corestate.DesiredDigest(step.Node.Desired)}, nil
}

type userState struct {
	exists       bool
	uid          string
	gidNum       string
	primaryGroup string
	home         string
	shell        string
	groups       []string
}

func (p NativeProvider) readUser(ctx context.Context, host, name string) (userState, error) {
	quoted := shellQuote(name)
	script := "if getent passwd " + quoted + " >/dev/null 2>&1; then\n" +
		"  getent passwd " + quoted + "\n" +
		"  id -gn " + quoted + "\n" +
		"  id -nG " + quoted + "\n" +
		"else\n" +
		"  echo __ABSENT__\n" +
		"fi\n"
	result, err := p.Runner.Run(ctx, host, script)
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
		for _, group := range strings.Fields(lines[2]) {
			if group != st.primaryGroup {
				st.groups = append(st.groups, group)
			}
		}
	}
	return st, nil
}

func userFlags(node graph.Node) string {
	var flags string
	if uid := stringDesired(node, "uid"); uid != "" {
		flags += " -u " + shellQuote(uid)
	}
	if group := stringDesired(node, "group"); group != "" {
		flags += " -g " + shellQuote(group)
	}
	if groups := stringListDesired(node, "groups"); len(groups) > 0 {
		flags += " -G " + shellQuote(strings.Join(groups, ","))
	}
	if home := stringDesired(node, "home"); home != "" {
		flags += " -d " + shellQuote(home)
	}
	if shell := stringDesired(node, "shell"); shell != "" {
		flags += " -s " + shellQuote(shell)
	}
	return flags
}

func authorizedKeyPreamble(desired map[string]any) string {
	preamble := "user=" + shellQuote(stringMapValue(desired, "user")) + "\n" +
		"home=$(getent passwd \"$user\" | cut -d: -f6) || home=\n"
	if path := stringMapValue(desired, "path"); path != "" {
		preamble += "file=" + shellQuote(path) + "\n"
	} else {
		preamble += "file=\"$home/.ssh/authorized_keys\"\n"
	}
	return preamble
}

func splitAuthorizedKey(key string) (keytype, keyblob string, err error) {
	fields := strings.Fields(strings.TrimSpace(key))
	if len(fields) < 2 {
		return "", "", fmt.Errorf("authorized key must contain a type and body")
	}
	return fields[0], fields[1], nil
}

func userInGroup(user userState, group string) bool {
	return user.primaryGroup == group || stringSliceContains(user.groups, group)
}

func stringSliceContains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
