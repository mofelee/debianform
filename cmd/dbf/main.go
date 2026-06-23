package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/hashicorp/hcl/v2/hclwrite"
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
	case "component":
		return runComponent(args[1:])
	case "validate", "plan", "apply", "check":
		return runConfigCommand(cmd, args[1:])
	case "help", "-h", "--help":
		usage()
		return nil
	default:
		return fmt.Errorf("unknown command %q", cmd)
	}
}

func runComponent(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("component subcommand is required")
	}
	switch args[0] {
	case "inspect":
		return runComponentInspect(args[1:])
	default:
		return fmt.Errorf("unknown component subcommand %q", args[0])
	}
}

func runComponentInspect(args []string) error {
	fs := flag.NewFlagSet("component inspect", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	file := fs.String("f", "", "configuration file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	rest := fs.Args()
	if len(rest) != 1 {
		return fmt.Errorf("component inspect requires exactly one component name")
	}
	files, err := configFiles(*file)
	if err != nil {
		return err
	}
	cfg, err := v2parser.ParseFiles(files)
	if err != nil {
		return err
	}
	var warnings []v2ir.Warning
	program, err := compileV2Program(cfg, "", v2merge.CompileOptions{SkipComponents: true, Warnings: &warnings})
	if err != nil {
		return err
	}
	printWarnings(warnings)
	component, ok := program.Components[rest[0]]
	if !ok {
		return fmt.Errorf("unknown component.%s", rest[0])
	}
	inputs := make([]componentInspectInput, 0, len(component.Inputs))
	for _, name := range sortedComponentInputNames(component.Inputs) {
		input := component.Inputs[name]
		defaultValue := input.Default
		if input.Sensitive && defaultValue != nil {
			defaultValue = "<sensitive>"
		}
		inputs = append(inputs, componentInspectInput{
			Name:        input.Name,
			Type:        input.Type,
			Default:     defaultValue,
			Nullable:    input.Nullable,
			Sensitive:   input.Sensitive,
			Deprecated:  input.Deprecated,
			Description: input.Description,
		})
	}
	out := componentInspectOutput{
		Name:   component.Name,
		Inputs: inputs,
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(out)
}

type componentInspectOutput struct {
	Name   string                  `json:"name"`
	Inputs []componentInspectInput `json:"inputs"`
}

type componentInspectInput struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Default     any    `json:"default,omitempty"`
	Nullable    bool   `json:"nullable"`
	Sensitive   bool   `json:"sensitive"`
	Deprecated  string `json:"deprecated,omitempty"`
	Description string `json:"description,omitempty"`
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
	if _, err := loadV2Program(files, "", v2merge.CompileOptions{SkipComponents: true}); err != nil {
		return err
	}
	changed := 0
	for _, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		formatted := hclwrite.Format(data)
		if bytes.Equal(data, formatted) {
			continue
		}
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		if err := os.WriteFile(path, formatted, info.Mode().Perm()); err != nil {
			return err
		}
		changed++
	}
	fmt.Printf("formatted %d file(s)\n", changed)
	return nil
}

func runConfigCommand(cmd string, args []string) error {
	fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	file := fs.String("f", "", "configuration file")
	host := fs.String("host", "", "limit execution to a host")
	format := fs.String("format", "text", "plan output format: text or json")
	htmlPath := fs.String("html", "", "write plan as static HTML")
	debug := fs.Bool("debug", false, "show internal provider addresses in plan output")
	offline := fs.Bool("offline", false, "render plan without SSH, state, or runtime facts discovery")
	parallel := fs.Int("parallel", 1, "maximum number of hosts to apply concurrently")
	lockTimeout := fs.Duration("lock-timeout", 5*time.Minute, "state lock timeout")
	autoApprove := fs.Bool("auto-approve", false, "skip apply confirmation")
	if err := fs.Parse(args); err != nil {
		return err
	}

	files, err := configFiles(*file)
	if err != nil {
		return err
	}
	return runV2ConfigCommand(cmd, files, *host, *format, *htmlPath, *debug, *offline, *parallel, *lockTimeout, *autoApprove)
}

func runV2ConfigCommand(cmd string, files []string, host string, format string, htmlPath string, debug bool, offline bool, parallel int, lockTimeout time.Duration, autoApprove bool) error {
	if format == "" {
		format = "text"
	}
	if format != "text" && format != "json" {
		return fmt.Errorf("unsupported v2 plan format %q", format)
	}
	if htmlPath != "" && cmd != "plan" {
		return fmt.Errorf("--html is only supported for v2 plan")
	}
	if htmlPath != "" && format != "text" {
		return fmt.Errorf("--html cannot be combined with --format")
	}
	if debug && cmd != "plan" {
		return fmt.Errorf("--debug is only supported for v2 plan")
	}
	if offline && cmd != "plan" {
		return fmt.Errorf("--offline is only supported for v2 plan")
	}
	if parallel < 1 {
		return fmt.Errorf("--parallel must be at least 1")
	}
	if parallel != 1 && cmd != "apply" {
		return fmt.Errorf("--parallel is only supported for v2 apply")
	}

	cfg, err := v2parser.ParseFiles(files)
	if err != nil {
		return err
	}
	var warnings []v2ir.Warning

	switch cmd {
	case "validate":
		program, err := compileV2ValidationProgram(cfg, host, &warnings)
		if err != nil {
			return err
		}
		if format != "text" {
			return fmt.Errorf("--format is only supported for v2 plan")
		}
		printWarnings(warnings)
		fmt.Printf("v2 configuration is valid: %d host(s)\n", len(program.Hosts))
		return nil
	case "plan":
		var doc v2plan.Document
		if offline {
			program, err := compileV2Program(cfg, host, v2merge.CompileOptions{Warnings: &warnings})
			if err != nil {
				if isRuntimeFactCompileError(err) {
					return fmt.Errorf("offline plan cannot resolve runtime facts; run dbf plan without --offline or declare matching system facts: %w", err)
				}
				return err
			}
			resourceGraph, err := v2graph.Compile(program)
			if err != nil {
				return err
			}
			doc = v2plan.New(resourceGraph, v2plan.Options{
				CommandFile: commandFile(files),
				Host:        commandHost(program, host),
				Debug:       debug,
			})
		} else {
			program, runner, err := loadOnlineV2Program(context.Background(), cfg, host, &warnings)
			if err != nil {
				return err
			}
			resourceGraph, err := v2graph.Compile(program)
			if err != nil {
				return err
			}
			engine := v2engine.Engine{
				Backend:  v2engine.NewSSHBackend(runner),
				Provider: v2engine.NewNativeProvider(runner),
			}
			onlinePlan, err := engine.Plan(context.Background(), program, resourceGraph, v2engine.Options{Host: host})
			if err != nil {
				return err
			}
			doc = onlinePlan.Document(v2plan.Options{
				CommandFile: commandFile(files),
				Host:        commandHost(program, host),
				Debug:       debug,
			})
		}
		printWarnings(warnings)
		return printPlanDocument(doc, format, htmlPath)
	case "check", "apply":
		if format != "text" {
			return fmt.Errorf("--format is only supported for v2 plan")
		}
		program, runner, err := loadOnlineV2Program(context.Background(), cfg, host, &warnings)
		if err != nil {
			return err
		}
		resourceGraph, err := v2graph.Compile(program)
		if err != nil {
			return err
		}
		engine := v2engine.Engine{
			Backend:  v2engine.NewSSHBackend(runner),
			Provider: v2engine.NewNativeProvider(runner),
		}
		opts := v2engine.Options{Host: host, LockTimeout: lockTimeout, Parallel: parallel}
		onlinePlan, err := engine.Plan(context.Background(), program, resourceGraph, opts)
		if err != nil {
			return err
		}
		doc := onlinePlan.Document(v2plan.Options{
			CommandFile: commandFile(files),
			Host:        commandHost(program, host),
		})
		printWarnings(warnings)
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

func printPlanDocument(doc v2plan.Document, format string, htmlPath string) error {
	if htmlPath != "" {
		if err := writePlanHTML(htmlPath, doc); err != nil {
			return err
		}
		fmt.Printf("wrote HTML plan to %s\n", htmlPath)
		return nil
	}
	switch format {
	case "json":
		return v2plan.PrintJSON(os.Stdout, doc)
	default:
		v2plan.PrintText(os.Stdout, doc)
		return nil
	}
}

func printWarnings(warnings []v2ir.Warning) {
	for _, warning := range warnings {
		if warning.Source.File != "" {
			fmt.Fprintf(os.Stderr, "warning: %s:%d:%s: %s\n", warning.Source.File, warning.Source.Line, warning.Source.Path, warning.Message)
			continue
		}
		fmt.Fprintf(os.Stderr, "warning: %s\n", warning.Message)
	}
}

func sortedComponentInputNames(inputs map[string]v2ir.ComponentInputSpec) []string {
	names := make([]string, 0, len(inputs))
	for name := range inputs {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func loadV2Program(files []string, host string, opts v2merge.CompileOptions) (*v2ir.Program, error) {
	cfg, err := v2parser.ParseFiles(files)
	if err != nil {
		return nil, err
	}
	return compileV2Program(cfg, host, opts)
}

func compileV2Program(cfg *v2parser.Config, host string, opts v2merge.CompileOptions) (*v2ir.Program, error) {
	opts.HostFilter = host
	program, err := v2merge.CompileWithOptions(cfg, opts)
	if err != nil {
		return nil, err
	}
	if host != "" && len(program.Hosts) == 0 {
		return nil, fmt.Errorf("host %q not found", host)
	}
	return program, nil
}

func compileV2ValidationProgram(cfg *v2parser.Config, host string, warnings *[]v2ir.Warning) (*v2ir.Program, error) {
	return compileV2Program(cfg, host, v2merge.CompileOptions{ValidateRuntimeTemplates: true, Warnings: warnings})
}

func isRuntimeFactCompileError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	if strings.Contains(msg, "must declare system.architecture") {
		return true
	}
	return strings.Contains(msg, ".suites") && strings.Contains(msg, "non-empty")
}

func loadOnlineV2Program(ctx context.Context, cfg *v2parser.Config, host string, warnings *[]v2ir.Warning) (*v2ir.Program, *v2engine.SSHRunner, error) {
	base, err := compileV2Program(cfg, host, v2merge.CompileOptions{SkipComponents: true})
	if err != nil {
		return nil, nil, err
	}
	runner := v2engine.NewSSHRunner(v2engine.HostsFromProgram(base))
	facts, err := v2engine.DiscoverProgramFacts(ctx, runner, base, nil)
	if err != nil {
		return nil, nil, err
	}
	resolved, err := compileV2Program(cfg, host, v2merge.CompileOptions{HostFacts: facts, Warnings: warnings})
	if err != nil {
		return nil, nil, err
	}
	return resolved, runner, nil
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
  dbf plan     [-f file] [--host name] [--format text|json] [--html file] [--debug]
  dbf apply    [-f file] [--host name] [--parallel n] [--auto-approve]
  dbf check    [-f file] [--host name]
  dbf fmt      [-f file]
  dbf version

By default dbf loads all *.dbf.hcl files in the current directory, sorted by name.
Use -f to load exactly one configuration file.`)
}

func writePlanHTML(path string, doc v2plan.Document) error {
	if path == "" {
		return fmt.Errorf("html output path is required")
	}
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	return v2plan.PrintHTML(file, doc)
}
