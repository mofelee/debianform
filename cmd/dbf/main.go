package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/mofelee/debianform/internal/v1/config"
	"github.com/mofelee/debianform/internal/v1/engine"
	"github.com/mofelee/debianform/internal/v1/sshx"
	"github.com/mofelee/debianform/internal/v1/state"
	"github.com/mofelee/debianform/internal/version"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "dbf: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		usage()
		return nil
	}

	cmd := args[0]
	switch cmd {
	case "version", "--version", "-version":
		printVersion(cmd == "version")
		return nil
	case "fmt":
		return runFmt(args[1:])
	case "validate", "plan", "apply", "check":
		return runConfigCommand(cmd, args[1:])
	case "help", "-h", "--help":
		usage()
		return nil
	default:
		return fmt.Errorf("unknown command %q", cmd)
	}
}

func printVersion(detailed bool) {
	info := version.Current()
	if !detailed {
		fmt.Printf("dbf %s\n", info.Short())
		return
	}

	fmt.Printf("dbf %s\n", info.Short())
	fmt.Printf("commit: %s\n", info.Commit)
	fmt.Printf("built: %s\n", info.Date)
	fmt.Printf("go: %s\n", info.GoVersion)
	fmt.Printf("platform: %s\n", info.Platform)
}

func runFmt(args []string) error {
	fs := flag.NewFlagSet("fmt", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	file := fs.String("f", "", "configuration file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	files, err := configFiles(*file)
	if err != nil {
		return err
	}
	if _, err := config.Load(files); err != nil {
		return err
	}
	fmt.Println("fmt: no-op for MVP parser; configuration parsed successfully")
	return nil
}

func runConfigCommand(cmd string, args []string) error {
	fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	file := fs.String("f", "", "configuration file")
	host := fs.String("host", "", "limit execution to a host")
	lockTimeout := fs.Duration("lock-timeout", 5*time.Minute, "state lock timeout")
	autoApprove := fs.Bool("auto-approve", false, "skip apply confirmation")
	if err := fs.Parse(args); err != nil {
		return err
	}

	files, err := configFiles(*file)
	if err != nil {
		return err
	}
	cfg, err := config.Load(files)
	if err != nil {
		return err
	}

	if cmd == "validate" {
		fmt.Printf("configuration is valid: %d host(s), %d resource(s)\n", len(cfg.Hosts), len(cfg.Resources))
		return nil
	}

	runner := sshx.NewRunner(cfg.Hosts)
	backend := state.NewSSHBackend(cfg.State, runner)
	e := engine.New(cfg, runner, backend)
	ctx := context.Background()

	switch cmd {
	case "plan":
		plan, err := e.Plan(ctx, engine.Options{Host: *host})
		if err != nil {
			return err
		}
		engine.PrintPlan(os.Stdout, plan)
		return nil
	case "check":
		plan, err := e.Plan(ctx, engine.Options{Host: *host})
		if err != nil {
			return err
		}
		engine.PrintPlan(os.Stdout, plan)
		if plan.HasChanges() {
			return fmt.Errorf("remote state does not match configuration")
		}
		return nil
	case "apply":
		if !*autoApprove {
			preview, err := e.Plan(ctx, engine.Options{Host: *host})
			if err != nil {
				return err
			}
			engine.PrintPlan(os.Stdout, preview)
			if !preview.HasChanges() {
				return nil
			}
			if !confirmApply() {
				return fmt.Errorf("apply cancelled")
			}
		}
		plan, err := e.Apply(ctx, engine.Options{Host: *host, LockTimeout: *lockTimeout})
		if err != nil {
			return err
		}
		engine.PrintPlan(os.Stdout, plan)
		fmt.Println("apply complete")
		return nil
	default:
		return fmt.Errorf("unsupported command %q", cmd)
	}
}

func configFiles(file string) ([]string, error) {
	if file != "" {
		return []string{file}, nil
	}
	matches, err := filepath.Glob("*.dbf.hcl")
	if err != nil {
		return nil, err
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("no configuration file found; pass -f or create one or more *.dbf.hcl files in the current directory")
	}
	sort.Strings(matches)
	return matches, nil
}

func confirmApply() bool {
	fmt.Print("Apply these changes? Type yes to continue: ")
	var answer string
	if _, err := fmt.Fscan(os.Stdin, &answer); err != nil {
		return false
	}
	return strings.EqualFold(answer, "yes")
}

func usage() {
	fmt.Println(`dbf manages Debian hosts from .dbf.hcl files.

Usage:
  dbf validate [-f file]
  dbf plan     [-f file] [--host name]
  dbf apply    [-f file] [--host name] [--auto-approve]
  dbf check    [-f file] [--host name]
  dbf fmt      [-f file]
  dbf version

By default dbf loads all *.dbf.hcl files in the current directory, sorted by name.
Use -f to load exactly one configuration file.`)
}
