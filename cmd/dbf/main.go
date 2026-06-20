package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/hashicorp/hcl/v2/hclparse"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/mofelee/debianform/internal/v1/config"
	"github.com/mofelee/debianform/internal/v1/engine"
	"github.com/mofelee/debianform/internal/v1/sshx"
	"github.com/mofelee/debianform/internal/v1/state"
	v2engine "github.com/mofelee/debianform/internal/v2/engine"
	v2graph "github.com/mofelee/debianform/internal/v2/graph"
	v2ir "github.com/mofelee/debianform/internal/v2/ir"
	v2merge "github.com/mofelee/debianform/internal/v2/merge"
	v2parser "github.com/mofelee/debianform/internal/v2/parser"
	v2plan "github.com/mofelee/debianform/internal/v2/plan"
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
	format := fs.String("format", "text", "plan output format: text or json")
	lockTimeout := fs.Duration("lock-timeout", 5*time.Minute, "state lock timeout")
	autoApprove := fs.Bool("auto-approve", false, "skip apply confirmation")
	if err := fs.Parse(args); err != nil {
		return err
	}

	files, err := configFiles(*file)
	if err != nil {
		return err
	}

	isV2, err := looksLikeV2(files)
	if err != nil {
		return err
	}
	if isV2 {
		return runV2ConfigCommand(cmd, files, *host, *format, *lockTimeout, *autoApprove)
	}
	if *format != "text" {
		return fmt.Errorf("--format is only supported for v2 plan")
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

func runV2ConfigCommand(cmd string, files []string, host string, format string, lockTimeout time.Duration, autoApprove bool) error {
	if format == "" {
		format = "text"
	}
	if format != "text" && format != "json" {
		return fmt.Errorf("unsupported v2 plan format %q", format)
	}

	program, err := loadV2Program(files, host)
	if err != nil {
		return err
	}

	switch cmd {
	case "validate":
		if format != "text" {
			return fmt.Errorf("--format is only supported for v2 plan")
		}
		fmt.Printf("v2 configuration is valid: %d host(s)\n", len(program.Hosts))
		return nil
	case "plan":
		resourceGraph, err := v2graph.Compile(program)
		if err != nil {
			return err
		}
		doc := v2plan.New(resourceGraph, v2plan.Options{
			CommandFile: commandFile(files),
			Host:        commandHost(program, host),
		})
		switch format {
		case "json":
			return v2plan.PrintJSON(os.Stdout, doc)
		default:
			v2plan.PrintText(os.Stdout, doc)
			return nil
		}
	case "check", "apply":
		if format != "text" {
			return fmt.Errorf("--format is only supported for v2 plan")
		}
		resourceGraph, err := v2graph.Compile(program)
		if err != nil {
			return err
		}
		runner := sshx.NewRunner(v2Hosts(program))
		engine := v2engine.Engine{
			Backend:  v2engine.NewSSHBackend(runner),
			Provider: v2engine.NewV1Provider(runner),
		}
		opts := v2engine.Options{Host: host, LockTimeout: lockTimeout}
		onlinePlan, err := engine.Plan(context.Background(), program, resourceGraph, opts)
		if err != nil {
			return err
		}
		doc := onlinePlan.Document(v2plan.Options{
			CommandFile: commandFile(files),
			Host:        commandHost(program, host),
		})
		v2plan.PrintText(os.Stdout, doc)
		if cmd == "check" {
			if len(onlinePlan.Steps) > 0 || len(onlinePlan.Operations) > 0 {
				return fmt.Errorf("remote state does not match v2 configuration")
			}
			return nil
		}
		if len(onlinePlan.Steps) == 0 && len(onlinePlan.Operations) == 0 {
			return nil
		}
		if !autoApprove && !confirmApply() {
			return fmt.Errorf("apply cancelled")
		}
		applied, err := engine.Apply(context.Background(), program, resourceGraph, opts)
		if err != nil {
			return err
		}
		appliedDoc := applied.Document(v2plan.Options{
			CommandFile: commandFile(files),
			Host:        commandHost(program, host),
		})
		v2plan.PrintText(os.Stdout, appliedDoc)
		fmt.Println("apply complete")
		return nil
	default:
		return fmt.Errorf("unsupported command %q", cmd)
	}
}

func v2Hosts(program *v2ir.Program) map[string]config.Host {
	hosts := map[string]config.Host{}
	for _, host := range program.Hosts {
		hosts[host.Name] = config.Host{
			Name:          host.Name,
			Address:       host.SSH.Host,
			Port:          portString(host.SSH.Port),
			IdentityFile:  host.SSH.IdentityFile,
			SSHConfigHost: host.SSH.Host,
		}
	}
	return hosts
}

func portString(port int) string {
	if port == 0 || port == 22 {
		return ""
	}
	return strconv.Itoa(port)
}

func loadV2Program(files []string, host string) (*v2ir.Program, error) {
	cfg, err := v2parser.ParseFiles(files)
	if err != nil {
		return nil, err
	}
	program, err := v2merge.Compile(cfg)
	if err != nil {
		return nil, err
	}
	return filterV2Program(program, host)
}

func filterV2Program(program *v2ir.Program, host string) (*v2ir.Program, error) {
	if host == "" {
		return program, nil
	}
	for _, candidate := range program.Hosts {
		if candidate.Name == host {
			return &v2ir.Program{Hosts: []v2ir.HostSpec{candidate}}, nil
		}
	}
	return nil, fmt.Errorf("host %q not found", host)
}

func commandFile(files []string) string {
	if len(files) == 1 {
		return files[0]
	}
	return strings.Join(files, ",")
}

func commandHost(program *v2ir.Program, host string) string {
	if host != "" {
		return host
	}
	if len(program.Hosts) == 1 {
		return program.Hosts[0].Name
	}
	return ""
}

func looksLikeV2(files []string) (bool, error) {
	parser := hclparse.NewParser()
	for _, file := range files {
		parsed, diags := parser.ParseHCLFile(file)
		if diags.HasErrors() {
			return false, fmt.Errorf("%s", diags.Error())
		}
		body, ok := parsed.Body.(*hclsyntax.Body)
		if !ok {
			return false, fmt.Errorf("%s: unsupported HCL body type %T", file, parsed.Body)
		}
		for _, block := range body.Blocks {
			switch block.Type {
			case "profile", "component":
				return true, nil
			case "host":
				for name := range block.Body.Attributes {
					if name == "imports" || name == "components" {
						return true, nil
					}
				}
				for _, nested := range block.Body.Blocks {
					switch nested.Type {
					case "ssh", "state", "system", "kernel", "packages", "files", "secrets", "directories", "groups", "users", "systemd", "services", "assert":
						return true, nil
					}
				}
			}
		}
	}
	return false, nil
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
  dbf plan     [-f file] [--host name] [--format text|json]
  dbf apply    [-f file] [--host name] [--auto-approve]
  dbf check    [-f file] [--host name]
  dbf fmt      [-f file]
  dbf version

By default dbf loads all *.dbf.hcl files in the current directory, sorted by name.
Use -f to load exactly one configuration file.`)
}
