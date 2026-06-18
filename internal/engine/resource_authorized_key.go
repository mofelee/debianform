package engine

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/mofelee/debianform/internal/config"
	"github.com/mofelee/debianform/internal/sshx"
)

// authorizedKeyProvider manages a single SSH public key in a user's
// authorized_keys file. A key is identified by its type and base64 body, so a
// changed trailing comment is not treated as drift.
type authorizedKeyProvider struct{}

func (authorizedKeyProvider) Desired(res config.Resource) (Desired, error) {
	d := baseDesired(res)
	d.User = stringAttr(res, "user", "")
	d.Path = stringAttr(res, "path", "")
	d.Ensure = stringAttr(res, "ensure", "present")

	key := strings.TrimSpace(stringAttr(res, "key", ""))
	if key == "" {
		if source := stringAttr(res, "source", ""); source != "" {
			data, err := os.ReadFile(source)
			if err != nil {
				return d, fmt.Errorf("%s read source %s: %w", res.Address, source, err)
			}
			key = strings.TrimSpace(string(data))
		}
	}
	d.PublicKey = key
	if _, _, err := splitAuthorizedKey(d.PublicKey); err != nil {
		return d, fmt.Errorf("%s %w", res.Address, err)
	}
	return d, nil
}

func (authorizedKeyProvider) Plan(ctx context.Context, e *Engine, res config.Resource, d Desired) (Change, error) {
	keytype, keyblob, err := splitAuthorizedKey(d.PublicKey)
	if err != nil {
		return Change{}, err
	}

	script := authorizedKeyPreamble(d) +
		"if [ -n \"$home\" ] && [ -f \"$file\" ] && awk -v t=" + sshx.ShellQuote(keytype) +
		" -v b=" + sshx.ShellQuote(keyblob) +
		" '($1==t && $2==b){f=1} END{exit f?0:1}' \"$file\"; then\n" +
		"  echo present\n" +
		"else\n" +
		"  echo absent\n" +
		"fi\n"
	result, err := e.runner.Run(ctx, res.Host, script)
	if err != nil {
		return Change{}, err
	}
	present := strings.Contains(result.Stdout, "present")

	if d.Ensure == "absent" {
		if present {
			return change(res, d, "delete", "remove authorized key for "+d.User), nil
		}
		return noChange(res, d), nil
	}
	if !present {
		return change(res, d, "create", "add authorized key for "+d.User), nil
	}
	return noChange(res, d), nil
}

func (authorizedKeyProvider) Apply(ctx context.Context, e *Engine, change Change) error {
	d := change.Desired
	keytype, keyblob, err := splitAuthorizedKey(d.PublicKey)
	if err != nil {
		return err
	}

	match := "awk -v t=" + sshx.ShellQuote(keytype) + " -v b=" + sshx.ShellQuote(keyblob)

	var body string
	if d.Ensure == "absent" {
		body = "if [ -f \"$file\" ]; then\n" +
			"  tmp=$(mktemp)\n" +
			"  " + match + " '!($1==t && $2==b)' \"$file\" > \"$tmp\"\n" +
			"  cat \"$tmp\" > \"$file\"\n" +
			"  rm -f \"$tmp\"\n" +
			"fi\n"
	} else {
		body = "dir=$(dirname \"$file\")\n" +
			"group=$(id -gn " + sshx.ShellQuote(d.User) + ")\n" +
			"mkdir -p \"$dir\"\n" +
			"chmod 0700 \"$dir\"\n" +
			"if ! { [ -f \"$file\" ] && " + match + " '($1==t && $2==b){f=1} END{exit f?0:1}' \"$file\"; }; then\n" +
			"  printf '%s\\n' " + sshx.ShellQuote(d.PublicKey) + " >> \"$file\"\n" +
			"fi\n" +
			"chmod 0600 \"$file\"\n" +
			"chown " + sshx.ShellQuote(d.User) + ":\"$group\" \"$dir\" \"$file\"\n"
	}

	script := "set -eu\n" + authorizedKeyPreamble(d) + body
	_, err = e.runner.Run(ctx, change.Resource.Host, script)
	return err
}

// authorizedKeyPreamble emits shell that resolves the user's home directory and
// the target authorized_keys file into $user, $home, and $file.
func authorizedKeyPreamble(d Desired) string {
	preamble := "user=" + sshx.ShellQuote(d.User) + "\n" +
		"home=$(getent passwd \"$user\" | cut -d: -f6) || home=\n"
	if d.Path != "" {
		preamble += "file=" + sshx.ShellQuote(d.Path) + "\n"
	} else {
		preamble += "file=\"$home/.ssh/authorized_keys\"\n"
	}
	return preamble
}

// splitAuthorizedKey returns the key type and base64 body, ignoring any
// trailing comment. It rejects keys that do not have at least a type and body.
func splitAuthorizedKey(key string) (keytype, keyblob string, err error) {
	fields := strings.Fields(key)
	if len(fields) < 2 {
		return "", "", fmt.Errorf("authorized key must contain a type and body")
	}
	return fields[0], fields[1], nil
}
