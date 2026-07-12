package graph

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/mofelee/debianform/internal/core/ir"
)

func scriptOutputPayloads(hostName, componentName, scriptName string, script ir.ComponentScriptSpec) []ScriptOutputPayload {
	paths := script.Outputs
	if len(paths) == 0 {
		return nil
	}
	out := make([]ScriptOutputPayload, 0, len(paths))
	componentPrefix := fmt.Sprintf("host.%s.components.%s", hostName, componentName)
	digest := componentScriptDigest(script)
	for _, path := range paths {
		out = append(out, ScriptOutputPayload{
			Address:         fmt.Sprintf("%s.script[%s].outputs[%s]", componentPrefix, strconv.Quote(scriptName), strconv.Quote(path)),
			Path:            path,
			ScriptDigest:    digest,
			ProviderAddress: "component_script_output." + providerName(hostName, componentName, scriptName, path),
		})
	}
	return out
}

func hostScriptOutputPayloads(hostName, scriptName string, script ir.ComponentScriptSpec) []ScriptOutputPayload {
	paths := script.Outputs
	if len(paths) == 0 {
		return nil
	}
	out := make([]ScriptOutputPayload, 0, len(paths))
	prefix := fmt.Sprintf("host.%s.script[%s]", hostName, strconv.Quote(scriptName))
	digest := componentScriptDigest(script)
	for _, path := range paths {
		out = append(out, ScriptOutputPayload{
			Address:         fmt.Sprintf("%s.outputs[%s]", prefix, strconv.Quote(path)),
			Path:            path,
			ScriptDigest:    digest,
			ProviderAddress: "component_script_output." + providerName(hostName, "host", scriptName, path),
		})
	}
	return out
}

func componentScriptDigest(script ir.ComponentScriptSpec) string {
	data, err := json.Marshal(map[string]any{
		"name":        script.Name,
		"mode":        script.Mode,
		"body":        script.Body,
		"interpreter": script.Interpreter,
		"run":         script.Run,
		"content":     script.Content,
		"commands":    script.Commands,
	})
	if err != nil {
		return shortHash(script.Name + "\x00" + script.Run + "\x00" + script.Content)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func scriptPayloadKind(script ir.ComponentScriptSpec) string {
	if script.Body != "" {
		return script.Body
	}
	switch {
	case len(script.Commands) > 0:
		return "commands"
	case script.Content != "":
		return "content"
	default:
		return "run"
	}
}

func componentArtifactSourceLabel(source ir.ComponentArtifactSourceSpec) string {
	if source.Architecture != "" {
		return source.Architecture
	}
	return "default"
}

func componentArtifactInstallKind(artifactType string) string {
	switch artifactType {
	case "archive":
		return "component_archive"
	case "file":
		return "component_file"
	case "ca_certificate":
		return "component_ca_certificate"
	case "source":
		return "component_binary"
	default:
		return "component_binary"
	}
}

func componentArtifactInstallSummary(component ir.ComponentInstanceSpec) string {
	artifact := component.ArtifactType
	if artifact == "ca_certificate" {
		artifact = "ca certificate"
	}
	return "install component " + component.Name + " " + artifact + " " + component.Install.Path
}

func componentArtifactCachePath(component ir.ComponentInstanceSpec) string {
	if component.SelectedSource == nil {
		return ""
	}
	name := providerName(component.Name)
	if component.Template != "" && component.Template != component.Name {
		name = providerName(component.Template, component.Name)
	}
	return "/var/cache/debianform/components/" + name + "/" + component.SelectedSource.SHA256 + "/source"
}

func componentArtifactBuildPath(component ir.ComponentInstanceSpec) string {
	if component.SelectedSource == nil {
		return ""
	}
	name := providerName(component.Name)
	if component.Template != "" && component.Template != component.Name {
		name = providerName(component.Template, component.Name)
	}
	return "/var/cache/debianform/components/" + name + "/" + component.SelectedSource.SHA256 + "/build"
}

func componentArtifactBuildOutputPath(component ir.ComponentInstanceSpec) string {
	if component.Build == nil {
		return ""
	}
	return componentArtifactBuildPath(component) + "/out/" + shortHash(component.Build.Output) + "/" + component.Build.Output
}

func cloneCommandMatrix(in [][]string) [][]string {
	if len(in) == 0 {
		return nil
	}
	out := make([][]string, 0, len(in))
	for _, command := range in {
		out = append(out, append([]string(nil), command...))
	}
	return out
}
