package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/hashicorp/hcl/v2/hclwrite"
	coreengine "github.com/mofelee/debianform/internal/core/engine"
	coregraph "github.com/mofelee/debianform/internal/core/graph"
	coreir "github.com/mofelee/debianform/internal/core/ir"
	coremerge "github.com/mofelee/debianform/internal/core/merge"
	coreparser "github.com/mofelee/debianform/internal/core/parser"
	coreplan "github.com/mofelee/debianform/internal/core/plan"
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
	case "variable":
		return runVariable(args[1:])
	case "validate", "plan", "apply", "check":
		return runConfigCommand(cmd, args[1:])
	case "help", "-h", "--help":
		usage()
		return nil
	default:
		return fmt.Errorf("unknown command %q", cmd)
	}
}

func runVariable(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("variable subcommand is required")
	}
	switch args[0] {
	case "inspect":
		return runVariableInspect(args[1:])
	default:
		return fmt.Errorf("unknown variable subcommand %q", args[0])
	}
}

func runVariableInspect(args []string) error {
	fs := flag.NewFlagSet("variable inspect", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var filesFlag fileFlags
	var cliVars repeatedFlag
	var cliVarFiles repeatedFlag
	fs.Var(&filesFlag, "f", "configuration file; may be repeated")
	fs.Var(&cliVars, "var", "set a variable value as name=value")
	fs.Var(&cliVarFiles, "var-file", "load variable values from a .dbfvars or .dbfvars.json file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if len(fs.Args()) != 0 {
		return fmt.Errorf("variable inspect does not accept positional arguments")
	}
	files, err := configFiles(filesFlag)
	if err != nil {
		return err
	}
	cfg, err := parseConfigWithExternalValues(files, cliVarFiles, cliVars, coreparser.ParseOptions{
		AllowMissingVariables: true,
		SkipTopLevel:          true,
	})
	if err != nil {
		return err
	}
	var warnings []coreir.Warning
	program, err := compileProgram(cfg, "", coremerge.CompileOptions{SkipComponents: true, Warnings: &warnings})
	if err != nil {
		return err
	}
	printWarnings(warnings)
	variables := make([]variableInspectVariable, 0, len(program.Variables))
	for _, name := range sortedVariableSpecNames(program.Variables) {
		variable := program.Variables[name]
		defaultValue := variable.Default
		if variable.Sensitive && defaultValue != nil {
			defaultValue = "<sensitive>"
		}
		variables = append(variables, variableInspectVariable{
			Name:        variable.Name,
			Type:        variable.Type,
			Default:     defaultValue,
			Nullable:    variable.Nullable,
			Sensitive:   variable.Sensitive,
			Ephemeral:   variable.Ephemeral,
			Deprecated:  variable.Deprecated,
			Description: variable.Description,
		})
	}
	out := variableInspectOutput{Variables: variables}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(out)
}

type variableInspectOutput struct {
	Variables []variableInspectVariable `json:"variables"`
}

type variableInspectVariable struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Default     any    `json:"default,omitempty"`
	Nullable    bool   `json:"nullable"`
	Sensitive   bool   `json:"sensitive"`
	Ephemeral   bool   `json:"ephemeral"`
	Deprecated  string `json:"deprecated,omitempty"`
	Description string `json:"description,omitempty"`
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
	var filesFlag fileFlags
	fs.Var(&filesFlag, "f", "configuration file; may be repeated")
	if err := fs.Parse(args); err != nil {
		return err
	}
	rest := fs.Args()
	if len(rest) != 1 {
		return fmt.Errorf("component inspect requires exactly one component name")
	}
	files, err := configFiles(filesFlag)
	if err != nil {
		return err
	}
	cfg, err := coreparser.ParseFiles(files)
	if err != nil {
		return err
	}
	var warnings []coreir.Warning
	program, err := compileProgram(cfg, "", coremerge.CompileOptions{SkipComponents: true, Warnings: &warnings})
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
	var filesFlag fileFlags
	fs.Var(&filesFlag, "f", "configuration file; may be repeated")
	if err := fs.Parse(args); err != nil {
		return err
	}
	files, err := configFiles(filesFlag)
	if err != nil {
		return err
	}
	if _, err := loadProgram(files, "", coremerge.CompileOptions{SkipComponents: true}); err != nil {
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
	var filesFlag fileFlags
	host := fs.String("host", "", "limit execution to a host")
	format := fs.String("format", "text", "plan output format: text or json")
	htmlPath := fs.String("html", "", "write plan as static HTML")
	debug := fs.Bool("debug", false, "show internal provider addresses in plan output")
	offline := fs.Bool("offline", false, "render plan without SSH, state, or runtime facts discovery")
	parallel := fs.Int("parallel", 1, "maximum number of hosts to apply concurrently")
	lockTimeout := fs.Duration("lock-timeout", 5*time.Minute, "state lock timeout")
	autoApprove := fs.Bool("auto-approve", false, "skip apply confirmation")
	var cliVars repeatedFlag
	var cliVarFiles repeatedFlag
	fs.Var(&filesFlag, "f", "configuration file; may be repeated")
	fs.Var(&cliVars, "var", "set a variable value as name=value")
	fs.Var(&cliVarFiles, "var-file", "load variable values from a .dbfvars or .dbfvars.json file")
	if err := fs.Parse(args); err != nil {
		return err
	}

	files, err := configFiles(filesFlag)
	if err != nil {
		return err
	}
	return runConfigWorkflow(cmd, files, *host, *format, *htmlPath, *debug, *offline, *parallel, *lockTimeout, *autoApprove, cliVars, cliVarFiles)
}

type fileFlags []string

func (f *fileFlags) String() string {
	return strings.Join(*f, ",")
}

func (f *fileFlags) Set(value string) error {
	*f = append(*f, value)
	return nil
}

type repeatedFlag []string

func (f *repeatedFlag) String() string {
	return strings.Join(*f, ",")
}

func (f *repeatedFlag) Set(value string) error {
	*f = append(*f, value)
	return nil
}

func runConfigWorkflow(cmd string, files []string, host string, format string, htmlPath string, debug bool, offline bool, parallel int, lockTimeout time.Duration, autoApprove bool, cliVars []string, cliVarFiles []string) error {
	if format == "" {
		format = "text"
	}
	if format != "text" && format != "json" {
		return fmt.Errorf("unsupported plan format %q", format)
	}
	if htmlPath != "" && cmd != "plan" {
		return fmt.Errorf("--html is only supported for plan")
	}
	if htmlPath != "" && format != "text" {
		return fmt.Errorf("--html cannot be combined with --format")
	}
	if debug && cmd != "plan" {
		return fmt.Errorf("--debug is only supported for plan")
	}
	if offline && cmd != "plan" {
		return fmt.Errorf("--offline is only supported for plan")
	}
	if parallel < 1 {
		return fmt.Errorf("--parallel must be at least 1")
	}
	if parallel != 1 && cmd != "apply" {
		return fmt.Errorf("--parallel is only supported for apply")
	}

	cfg, err := parseConfigWithExternalValues(files, cliVarFiles, cliVars, coreparser.ParseOptions{})
	if err != nil {
		return err
	}
	var warnings []coreir.Warning

	switch cmd {
	case "validate":
		program, err := compileValidationProgram(cfg, host, &warnings)
		if err != nil {
			return err
		}
		if format != "text" {
			return fmt.Errorf("--format is only supported for plan")
		}
		printWarnings(warnings)
		fmt.Printf("configuration is valid: %d host(s)\n", len(program.Hosts))
		return nil
	case "plan":
		var doc coreplan.Document
		if offline {
			program, err := compileProgram(cfg, host, coremerge.CompileOptions{Warnings: &warnings})
			if err != nil {
				if isRuntimeFactCompileError(err) {
					return fmt.Errorf("offline plan cannot resolve runtime facts; run dbf plan without --offline or declare matching system facts: %w", err)
				}
				return err
			}
			resourceGraph, err := coregraph.Compile(program)
			if err != nil {
				if isRuntimeFactCompileError(err) {
					return fmt.Errorf("offline plan cannot resolve runtime facts; run dbf plan without --offline or declare matching system facts: %w", err)
				}
				return err
			}
			doc = coreplan.New(resourceGraph, coreplan.Options{
				CommandFile: commandFile(files),
				Host:        commandHost(program, host),
				Debug:       debug,
			})
		} else {
			program, runner, err := loadOnlineProgramWithProgress(context.Background(), cfg, host, &warnings, os.Stderr)
			if err != nil {
				return err
			}
			resourceGraph, err := coregraph.Compile(program)
			if err != nil {
				return err
			}
			engine := coreengine.Engine{
				Backend:  coreengine.NewSSHBackend(runner),
				Provider: coreengine.NewNativeProvider(runner),
			}
			onlinePlan, err := engine.Plan(context.Background(), program, resourceGraph, coreengine.Options{Host: host, Progress: os.Stderr})
			if err != nil {
				return err
			}
			doc = onlinePlan.Document(coreplan.Options{
				CommandFile: commandFile(files),
				Host:        commandHost(program, host),
				Debug:       debug,
			})
		}
		printWarnings(warnings)
		return printPlanDocument(doc, format, htmlPath)
	case "check", "apply":
		if format != "text" {
			return fmt.Errorf("--format is only supported for plan")
		}
		program, runner, err := loadOnlineProgramWithProgress(context.Background(), cfg, host, &warnings, os.Stderr)
		if err != nil {
			return err
		}
		resourceGraph, err := coregraph.Compile(program)
		if err != nil {
			return err
		}
		engine := coreengine.Engine{
			Backend:  coreengine.NewSSHBackend(runner),
			Provider: coreengine.NewNativeProvider(runner),
		}
		opts := coreengine.Options{Host: host, LockTimeout: lockTimeout, Parallel: parallel, Progress: os.Stderr}
		onlinePlan, err := engine.Plan(context.Background(), program, resourceGraph, opts)
		if err != nil {
			return err
		}
		doc := onlinePlan.Document(coreplan.Options{
			CommandFile: commandFile(files),
			Host:        commandHost(program, host),
		})
		printWarnings(warnings)
		coreplan.PrintText(os.Stdout, doc)
		if cmd == "check" {
			if len(onlinePlan.Steps) > 0 || len(onlinePlan.Operations) > 0 {
				return fmt.Errorf("remote state does not match configuration")
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
		appliedDoc := applied.Document(coreplan.Options{
			CommandFile: commandFile(files),
			Host:        commandHost(program, host),
		})
		coreplan.PrintText(os.Stdout, appliedDoc)
		fmt.Println("apply complete")
		return nil
	default:
		return fmt.Errorf("unsupported command %q", cmd)
	}
}

func printPlanDocument(doc coreplan.Document, format string, htmlPath string) error {
	if htmlPath != "" {
		if err := writePlanHTML(htmlPath, doc); err != nil {
			return err
		}
		fmt.Printf("wrote HTML plan to %s\n", htmlPath)
		return nil
	}
	switch format {
	case "json":
		return coreplan.PrintJSON(os.Stdout, doc)
	default:
		coreplan.PrintText(os.Stdout, doc)
		return nil
	}
}

func parseConfigWithExternalValues(files []string, cliVarFiles []string, cliVars []string, opts coreparser.ParseOptions) (*coreparser.Config, error) {
	declarations, err := coreparser.ParseFilesWithOptions(files, coreparser.ParseOptions{
		AllowMissingVariables: true,
		SkipTopLevel:          true,
	})
	if err != nil {
		return nil, err
	}
	variableValues, err := collectExternalVariableValues(files, cliVarFiles, cliVars, declarations.Variables)
	if err != nil {
		return nil, err
	}
	opts.VariableValues = variableValues
	return coreparser.ParseFilesWithOptions(files, opts)
}

func collectExternalVariableValues(files []string, cliVarFiles []string, cliVars []string, variables map[string]coreparser.Variable) ([]coreparser.ExternalVariableValue, error) {
	values := parseEnvVars(os.Environ())
	autoVarFiles, err := autoVariableFiles(files)
	if err != nil {
		return nil, err
	}
	for _, path := range autoVarFiles {
		fileValues, err := coreparser.ParseVariableFile(path)
		if err != nil {
			return nil, err
		}
		values = append(values, fileValues...)
	}
	for _, path := range cliVarFiles {
		fileValues, err := coreparser.ParseVariableFile(path)
		if err != nil {
			return nil, err
		}
		values = append(values, fileValues...)
	}
	parsedCLIVars, err := parseCLIVars(cliVars, variables)
	if err != nil {
		return nil, err
	}
	values = append(values, parsedCLIVars...)
	return values, nil
}

func parseEnvVars(environ []string) []coreparser.ExternalVariableValue {
	const prefix = "DBF_VAR_"
	values := []coreparser.ExternalVariableValue{}
	for _, item := range environ {
		nameValue := strings.SplitN(item, "=", 2)
		if len(nameValue) != 2 || !strings.HasPrefix(nameValue[0], prefix) {
			continue
		}
		name := strings.TrimPrefix(nameValue[0], prefix)
		if name == "" {
			continue
		}
		values = append(values, coreparser.ExternalVariableValue{
			Name:          name,
			Value:         nameValue[1],
			Source:        coreir.SourceRef{File: "env", Line: 1, Path: nameValue[0]},
			IgnoreUnknown: true,
		})
	}
	sort.SliceStable(values, func(i, j int) bool {
		return values[i].Source.Path < values[j].Source.Path
	})
	return values
}

func autoVariableFiles(files []string) ([]string, error) {
	if len(files) == 0 {
		return nil, nil
	}
	dir := filepath.Dir(files[0])
	if dir == "" {
		dir = "."
	}
	for _, file := range files[1:] {
		otherDir := filepath.Dir(file)
		if otherDir == "" {
			otherDir = "."
		}
		if otherDir != dir {
			return nil, fmt.Errorf("automatic var files require configuration files from one directory; got %s and %s", dir, otherDir)
		}
	}
	defaultFiles := []string{}
	for _, name := range []string{"debianform.dbfvars", "debianform.dbfvars.json"} {
		path := filepath.Join(dir, name)
		if _, err := os.Stat(path); err == nil {
			defaultFiles = append(defaultFiles, path)
		} else if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
	}
	autoFiles, err := filepath.Glob(filepath.Join(dir, "*.auto.dbfvars"))
	if err != nil {
		return nil, err
	}
	autoJSONFiles, err := filepath.Glob(filepath.Join(dir, "*.auto.dbfvars.json"))
	if err != nil {
		return nil, err
	}
	autoFiles = append(autoFiles, autoJSONFiles...)
	sort.Strings(autoFiles)

	out := make([]string, 0, len(defaultFiles)+len(autoFiles))
	out = append(out, defaultFiles...)
	out = append(out, autoFiles...)
	return out, nil
}

func parseCLIVars(values []string, variables map[string]coreparser.Variable) ([]coreparser.ExternalVariableValue, error) {
	out := make([]coreparser.ExternalVariableValue, 0, len(values))
	for i, raw := range values {
		name, value, ok := strings.Cut(raw, "=")
		name = strings.TrimSpace(name)
		source := coreir.SourceRef{File: "cli", Line: i + 1, Path: fmt.Sprintf("cli.var[%d]", i)}
		if !ok || name == "" {
			return nil, fmt.Errorf("%s:%d:%s: -var must be name=value", source.File, source.Line, source.Path)
		}
		variable, known := variables[name]
		if known && strings.HasPrefix(value, "@") {
			loaded, loadedSource, err := readCLIVarAtSource(value, source, known && variable.Sensitive)
			if err != nil {
				return nil, err
			}
			value = loaded
			source = loadedSource
		} else if known && strings.HasPrefix(value, "env:") {
			loaded, loadedSource, err := readCLIVarEnvSource(value, source, known && variable.Sensitive)
			if err != nil {
				return nil, err
			}
			value = loaded
			source = loadedSource
		}
		out = append(out, coreparser.ExternalVariableValue{
			Name:   name,
			Value:  value,
			Source: source,
		})
	}
	return out, nil
}

func readCLIVarAtSource(value string, source coreir.SourceRef, sensitive bool) (string, coreir.SourceRef, error) {
	path := strings.TrimPrefix(value, "@")
	if path == "" {
		return "", source, fmt.Errorf("%s:%d:%s: -var @ source path must be non-empty", source.File, source.Line, source.Path)
	}
	if path == "-" {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", source, fmt.Errorf("%s:%d:%s: failed to read -var from stdin: %w", source.File, source.Line, source.Path, err)
		}
		source.Path = sourcePath("stdin", sensitive)
		return string(data), source, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if sensitive {
			return "", source, fmt.Errorf("%s:%d:%s: failed to read -var source %s: %s", source.File, source.Line, source.Path, sourcePath(path, sensitive), pathlessError(err))
		}
		return "", source, fmt.Errorf("%s:%d:%s: failed to read -var source %s: %w", source.File, source.Line, source.Path, sourcePath(path, sensitive), err)
	}
	source.Path = sourcePath(path, sensitive)
	return string(data), source, nil
}

func readCLIVarEnvSource(value string, source coreir.SourceRef, sensitive bool) (string, coreir.SourceRef, error) {
	name := strings.TrimPrefix(value, "env:")
	if name == "" {
		return "", source, fmt.Errorf("%s:%d:%s: -var env source name must be non-empty", source.File, source.Line, source.Path)
	}
	loaded, ok := os.LookupEnv(name)
	if !ok {
		return "", source, fmt.Errorf("%s:%d:%s: -var env source %s is not set", source.File, source.Line, source.Path, sourcePath(name, sensitive))
	}
	source.Path = sourcePath("env:"+name, sensitive)
	return loaded, source, nil
}

func sourcePath(path string, sensitive bool) string {
	if sensitive {
		return "<sensitive-source>"
	}
	return path
}

func pathlessError(err error) string {
	if pathErr, ok := err.(*os.PathError); ok {
		return pathErr.Err.Error()
	}
	return err.Error()
}

func printWarnings(warnings []coreir.Warning) {
	for _, warning := range warnings {
		if warning.Source.File != "" {
			fmt.Fprintf(os.Stderr, "warning: %s:%d:%s: %s\n", warning.Source.File, warning.Source.Line, warning.Source.Path, warning.Message)
			continue
		}
		fmt.Fprintf(os.Stderr, "warning: %s\n", warning.Message)
	}
}

func sortedComponentInputNames(inputs map[string]coreir.ComponentInputSpec) []string {
	names := make([]string, 0, len(inputs))
	for name := range inputs {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func sortedVariableSpecNames(variables map[string]coreir.VariableSpec) []string {
	names := make([]string, 0, len(variables))
	for name := range variables {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func loadProgram(files []string, host string, opts coremerge.CompileOptions) (*coreir.Program, error) {
	cfg, err := coreparser.ParseFiles(files)
	if err != nil {
		return nil, err
	}
	return compileProgram(cfg, host, opts)
}

func compileProgram(cfg *coreparser.Config, host string, opts coremerge.CompileOptions) (*coreir.Program, error) {
	opts.HostFilter = host
	program, err := coremerge.CompileWithOptions(cfg, opts)
	if err != nil {
		return nil, err
	}
	if host != "" && len(program.Hosts) == 0 {
		return nil, fmt.Errorf("host %q not found", host)
	}
	return program, nil
}

func compileValidationProgram(cfg *coreparser.Config, host string, warnings *[]coreir.Warning) (*coreir.Program, error) {
	return compileProgram(cfg, host, coremerge.CompileOptions{ValidateRuntimeTemplates: true, Warnings: warnings})
}

func isRuntimeFactCompileError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	if strings.Contains(msg, "must declare system.architecture") {
		return true
	}
	if strings.Contains(msg, "must declare system.codename") {
		return true
	}
	return strings.Contains(msg, ".suites") && strings.Contains(msg, "non-empty")
}

func loadOnlineProgram(ctx context.Context, cfg *coreparser.Config, host string, warnings *[]coreir.Warning) (*coreir.Program, *coreengine.SSHRunner, error) {
	return loadOnlineProgramWithProgress(ctx, cfg, host, warnings, nil)
}

func loadOnlineProgramWithProgress(ctx context.Context, cfg *coreparser.Config, host string, warnings *[]coreir.Warning, progress io.Writer) (*coreir.Program, *coreengine.SSHRunner, error) {
	base, err := compileProgram(cfg, host, coremerge.CompileOptions{SkipComponents: true})
	if err != nil {
		return nil, nil, err
	}
	runner := coreengine.NewSSHRunner(coreengine.HostsFromProgram(base))
	facts, err := coreengine.DiscoverProgramFactsWithProgress(ctx, runner, base, nil, progress)
	if err != nil {
		return nil, nil, err
	}
	resolved, err := compileProgram(cfg, host, coremerge.CompileOptions{HostFacts: facts, Warnings: warnings})
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

func commandHost(program *coreir.Program, host string) string {
	if host != "" {
		return host
	}
	if len(program.Hosts) == 1 {
		return program.Hosts[0].Name
	}
	return ""
}

func configFiles(files []string) ([]string, error) {
	if len(files) != 0 {
		return append([]string(nil), files...), nil
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
  dbf validate [-f file ...] [-var name=value] [-var-file path] [--host name]
  dbf plan     [-f file ...] [-var name=value] [-var-file path] [--host name] [--format text|json] [--html file] [--debug] [--offline]
  dbf apply    [-f file ...] [-var name=value] [-var-file path] [--host name] [--parallel n] [--lock-timeout duration] [--auto-approve]
  dbf check    [-f file ...] [-var name=value] [-var-file path] [--host name] [--lock-timeout duration]
  dbf variable inspect [-f file ...] [-var name=value] [-var-file path]
  dbf component inspect [-f file ...] name
  dbf fmt      [-f file ...]
  dbf version

By default dbf loads all *.dbf.hcl files in the current directory, sorted by name.
Use -f file one or more times to load only the explicitly specified file(s).`)
}

func writePlanHTML(path string, doc coreplan.Document) error {
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
	return coreplan.PrintHTML(file, doc)
}
