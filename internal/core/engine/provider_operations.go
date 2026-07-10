package engine

import (
	"context"
	"fmt"
	"strings"

	"github.com/mofelee/debianform/internal/core/graph"
)

func (p NativeProvider) RunOperation(ctx context.Context, operation graph.Operation) (OperationResult, error) {
	action := operation.Action
	if action == "" {
		action = ActionRun
	}
	address := operation.Address
	if current, ok := RemoteCallContextFromContext(ctx); ok && current.Address != "" {
		address = current.Address
	}
	ctx = WithRemoteCallContext(ctx, RemoteCallContext{
		Phase:   "run operation",
		Address: address,
		Action:  action,
		Summary: operation.Summary,
	})
	if operation.ScriptPayload != nil {
		result, err := p.runScriptOperation(ctx, operation)
		if err != nil && operation.Sensitive {
			err = redactPayloadError(err)
		}
		return result, err
	}
	if operation.CommandPreview == "" {
		return OperationResult{}, nil
	}
	host := operation.Host
	if host == "" {
		return OperationResult{}, fmt.Errorf("operation %s is missing its target host", operation.Address)
	}
	_, err := p.Runner.RunCommand(ctx, host, operation.CommandPreview)
	if err != nil && operation.Sensitive {
		err = redactPayloadError(err)
	}
	return OperationResult{}, err
}

func (p NativeProvider) runScriptOperation(ctx context.Context, operation graph.Operation) (OperationResult, error) {
	host := operation.Host
	if host == "" {
		return OperationResult{}, fmt.Errorf("operation %s is missing its target host", operation.Address)
	}
	payload := operation.ScriptPayload
	if payload == nil {
		return OperationResult{}, nil
	}
	interpreter := append([]string(nil), payload.Interpreter...)
	if len(interpreter) == 0 {
		return OperationResult{}, fmt.Errorf("%s script payload requires interpreter", operation.Address)
	}
	for i, arg := range interpreter {
		if arg == "" {
			return OperationResult{}, fmt.Errorf("%s script payload interpreter[%d] must be non-empty", operation.Address, i)
		}
	}
	script, err := scriptPayloadContent(operation.Address, payload)
	if err != nil {
		return OperationResult{}, err
	}
	remoteCommand := scriptEnvironmentPrefix(payload) + strings.Join(shellQuoteArgs(interpreter), " ")
	_, err = p.Runner.RunInput(ctx, host, remoteCommand, strings.NewReader(script))
	if err != nil {
		return OperationResult{}, err
	}
	outputs, err := p.readScriptOutputs(ctx, host, payload.Outputs)
	if err != nil {
		return OperationResult{}, err
	}
	return OperationResult{Outputs: outputs}, nil
}

func (p NativeProvider) readScriptOutputs(ctx context.Context, host string, outputs []graph.ScriptOutputPayload) (map[string]map[string]any, error) {
	if len(outputs) == 0 {
		return nil, nil
	}
	out := make(map[string]map[string]any, len(outputs))
	for _, output := range outputs {
		callCtx := WithRemoteCallContext(ctx, RemoteCallContext{
			Phase:   "operation output read",
			Address: output.Address,
			Action:  "read",
			Summary: output.Path,
		})
		current, err := p.readPath(callCtx, host, output.Path)
		if err != nil {
			return nil, err
		}
		observed := current.observed()
		observed["path"] = output.Path
		if !current.Exists || current.IsDir || current.SHA256 == "" {
			return nil, fmt.Errorf("script output %s was not created as a regular file", output.Path)
		}
		out[output.Address] = observed
	}
	return out, nil
}

func scriptEnvironmentPrefix(payload *graph.ScriptPayload) string {
	if payload == nil {
		return ""
	}
	env := map[string]string{
		"DBF_SCRIPT_NAME":       payload.Name,
		"DBF_COMPONENT_NAME":    payload.ComponentName,
		"DBF_TRIGGER_ADDRESS":   firstString(payload.TriggerAddresses),
		"DBF_TRIGGER_PATH":      firstString(payload.TriggerPaths),
		"DBF_TRIGGER_ADDRESSES": strings.Join(payload.TriggerAddresses, "\n"),
		"DBF_TRIGGER_PATHS":     strings.Join(payload.TriggerPaths, "\n"),
	}
	keys := []string{
		"DBF_SCRIPT_NAME",
		"DBF_COMPONENT_NAME",
		"DBF_TRIGGER_ADDRESS",
		"DBF_TRIGGER_PATH",
		"DBF_TRIGGER_ADDRESSES",
		"DBF_TRIGGER_PATHS",
	}
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+shellQuote(env[key]))
	}
	return strings.Join(parts, " ") + " "
}

func firstString(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func scriptPayloadContent(address string, payload *graph.ScriptPayload) (string, error) {
	if payload == nil {
		return "", nil
	}
	switch payload.Kind {
	case "run":
		return payload.Run + "\n", nil
	case "content":
		return payload.Content, nil
	case "commands":
		return scriptPayloadCommands(address, payload.Commands)
	default:
		return "", fmt.Errorf("%s script payload kind must be run, content, or commands", address)
	}
}

func scriptPayloadCommands(address string, commands [][]string) (string, error) {
	if len(commands) == 0 {
		return "", fmt.Errorf("%s script payload commands must contain at least one command", address)
	}
	lines := make([]string, 0, len(commands))
	for i, command := range commands {
		if len(command) == 0 {
			return "", fmt.Errorf("%s script payload command[%d] must contain at least one argument", address, i)
		}
		for j, arg := range command {
			if arg == "" {
				return "", fmt.Errorf("%s script payload command[%d][%d] must be non-empty", address, i, j)
			}
		}
		lines = append(lines, strings.Join(shellQuoteArgs(command), " "))
	}
	return strings.Join(lines, "\n") + "\n", nil
}
