package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/mofelee/debianform/internal/core/graph"
	corestate "github.com/mofelee/debianform/internal/core/state"
)

func (p NativeProvider) planDockerComposeProject(ctx context.Context, node graph.Node, prior *corestate.Resource) (ProviderPlan, error) {
	observed, err := p.readDockerComposeProject(ctx, node)
	if err != nil {
		return ProviderPlan{}, err
	}
	want := stringDesired(node, "state")
	actual, _ := observed["state"].(string)
	project := stringDesired(node, "project")
	orphanCount := intMapValue(observed, "orphan_count")
	if want == "absent" {
		if actual == "absent" {
			return absentInSyncPlan(prior, "already absent docker compose project "+project, observed), nil
		}
		return ProviderPlan{Action: ActionDelete, Summary: "remove docker compose project " + project, Observed: observed, Ownership: ownership(prior)}, nil
	}
	if actual != want {
		return ProviderPlan{Action: ActionUpdate, Summary: dockerComposeDriftSummary(project, want, actual, orphanCount), Observed: observed, Ownership: ownership(prior)}, nil
	}
	if orphanCount > 0 {
		return ProviderPlan{Action: ActionUpdate, Summary: dockerComposeOrphanSummary(project, orphanCount, boolDesired(node, "remove_orphans")), Observed: observed, Ownership: ownership(prior)}, nil
	}
	desiredDigest := corestate.DesiredDigest(node.Desired)
	if prior != nil && prior.DesiredDigest != "" && prior.DesiredDigest != desiredDigest {
		observed = cloneMap(observed)
		observed["desired_digest"] = prior.DesiredDigest
		if oldProject := stringMapValue(prior.Desired, "project"); oldProject != "" && oldProject != project {
			return ProviderPlan{Action: ActionUpdate, Summary: "replace docker compose project " + oldProject + " with " + project, Observed: observed, Ownership: ownership(prior)}, nil
		}
		return ProviderPlan{Action: ActionUpdate, Summary: "update docker compose project " + project, Observed: observed, Ownership: ownership(prior)}, nil
	}
	return inSyncPlan(node, prior, "no changes for docker compose project "+project, observed), nil
}

func (p NativeProvider) applyDockerComposeProject(ctx context.Context, step Step) (map[string]any, error) {
	if oldProject := dockerComposePriorProject(step); oldProject != "" && oldProject != stringDesired(step.Node, "project") {
		command := dockerComposeProjectCommandForProject(step.Node, oldProject, "down")
		if _, err := p.Runner.Run(ctx, step.Node.Host, command); err != nil {
			return nil, err
		}
	}
	command, err := dockerComposeProjectCommand(step.Node)
	if err != nil {
		return nil, err
	}
	_, err = p.Runner.Run(ctx, step.Node.Host, command)
	if err != nil {
		return nil, err
	}
	observed, err := p.readDockerComposeProject(ctx, step.Node)
	if err != nil {
		return nil, err
	}
	observed["desired_digest"] = corestate.DesiredDigest(step.Node.Desired)
	return observed, nil
}

func (p NativeProvider) readDockerComposeProject(ctx context.Context, node graph.Node) (map[string]any, error) {
	expectedServices := []string{}
	servicesResult, err := p.Runner.Run(ctx, node.Host, dockerComposeProjectServicesCommand(node))
	if err != nil {
		return nil, err
	}
	expectedServices = dockerComposeConfigServices(servicesResult.Stdout)
	psResult, err := p.Runner.Run(ctx, node.Host, dockerComposeProjectPSCommand(node))
	if err != nil {
		return nil, err
	}
	return summarizeDockerComposePS(psResult.Stdout, expectedServices), nil
}

func dockerComposeProjectServicesCommand(node graph.Node) string {
	args := dockerComposeBaseArgs(node)
	args = append(args, "config", "--services")
	return strings.Join(shellQuoteArgs(args), " ") + " 2>/dev/null || true\n"
}

func dockerComposeProjectPSCommand(node graph.Node) string {
	args := dockerComposeBaseArgs(node)
	args = append(args, "ps", "--all", "--format", "json")
	return strings.Join(shellQuoteArgs(args), " ") + " 2>/dev/null || true\n"
}

func dockerComposeProjectCommand(node graph.Node) (string, error) {
	args := dockerComposeBaseArgs(node)
	switch stringDesired(node, "state") {
	case "running":
		args = append(args, "up", "-d")
		args = append(args, dockerComposePullArgs(stringDesired(node, "pull"))...)
		args = append(args, dockerComposeRecreateArgs(stringDesired(node, "recreate"))...)
		if boolDesired(node, "remove_orphans") {
			args = append(args, "--remove-orphans")
		}
	case "stopped":
		args = append(args, "stop")
	case "absent":
		args = append(args, "down")
		if boolDesired(node, "remove_orphans") {
			args = append(args, "--remove-orphans")
		}
	default:
		return "", fmt.Errorf("%s unsupported docker compose state %q", node.Address, stringDesired(node, "state"))
	}
	return strings.Join(shellQuoteArgs(args), " ") + "\n", nil
}

func dockerComposeProjectCommandForProject(node graph.Node, project string, command ...string) string {
	args := []string{"docker", "compose", "-p", project}
	for _, file := range stringListDesired(node, "files") {
		args = append(args, "-f", file)
	}
	args = append(args, command...)
	return strings.Join(shellQuoteArgs(args), " ") + "\n"
}

func dockerComposeBaseArgs(node graph.Node) []string {
	args := []string{"docker", "compose", "-p", stringDesired(node, "project")}
	for _, file := range stringListDesired(node, "files") {
		args = append(args, "-f", file)
	}
	return args
}

func dockerComposePullArgs(value string) []string {
	switch value {
	case "never":
		return []string{"--pull", "never"}
	case "always":
		return []string{"--pull", "always"}
	default:
		return []string{"--pull", "missing"}
	}
}

func dockerComposeRecreateArgs(value string) []string {
	switch value {
	case "always":
		return []string{"--force-recreate"}
	case "never":
		return []string{"--no-recreate"}
	default:
		return nil
	}
}

func shellQuoteArgs(args []string) []string {
	out := make([]string, 0, len(args))
	for _, arg := range args {
		out = append(out, shellQuote(arg))
	}
	return out
}

func summarizeDockerComposePS(stdout string, expectedServices []string) map[string]any {
	total := 0
	running := 0
	stopped := 0
	actualServices := []string{}
	for _, container := range dockerComposePSContainers(stdout) {
		total++
		if strings.EqualFold(container.State, "running") {
			running++
		} else {
			stopped++
		}
		if container.Service != "" {
			actualServices = append(actualServices, container.Service)
		}
	}
	expectedServices = dedupeStringValues(expectedServices)
	actualServices = dedupeStringValues(actualServices)
	state := dockerComposeProjectObservedState(total, running)
	orphanServices := dockerComposeOrphanServices(actualServices, expectedServices)
	return map[string]any{
		"exists":   total > 0,
		"state":    state,
		"services": map[string]any{"total": total, "running": running, "stopped": stopped, "expected": expectedServices, "actual": actualServices},
		"containers": map[string]any{
			"total": total,
		},
		"orphan_count":    len(orphanServices),
		"orphan_services": orphanServices,
	}
}

func dockerComposeProjectObservedState(total, running int) string {
	switch {
	case total == 0:
		return "absent"
	case running == total:
		return "running"
	case running == 0:
		return "stopped"
	default:
		return "degraded"
	}
}

type dockerComposeContainer struct {
	Service string
	State   string
	Name    string
}

func dockerComposePSContainers(stdout string) []dockerComposeContainer {
	text := strings.TrimSpace(stdout)
	if text == "" || text == "[]" {
		return nil
	}
	var array []map[string]any
	if err := json.Unmarshal([]byte(text), &array); err == nil {
		containers := make([]dockerComposeContainer, 0, len(array))
		for _, item := range array {
			if container := dockerComposePSContainer(item); container.State != "" {
				containers = append(containers, container)
			}
		}
		return containers
	}
	var containers []dockerComposeContainer
	lines := strings.Split(text, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || line == "[]" {
			continue
		}
		var item map[string]any
		if err := json.Unmarshal([]byte(line), &item); err == nil {
			if container := dockerComposePSContainer(item); container.State != "" {
				containers = append(containers, container)
			}
		}
	}
	return containers
}

func dockerComposePSContainer(item map[string]any) dockerComposeContainer {
	container := dockerComposeContainer{}
	for _, key := range []string{"State", "state"} {
		if state, ok := item[key].(string); ok {
			container.State = state
			break
		}
	}
	for _, key := range []string{"Service", "service"} {
		if service, ok := item[key].(string); ok {
			container.Service = service
			break
		}
	}
	for _, key := range []string{"Name", "name"} {
		if name, ok := item[key].(string); ok {
			container.Name = name
			break
		}
	}
	return container
}

func dockerComposeConfigServices(stdout string) []string {
	var services []string
	for _, line := range strings.Split(stdout, "\n") {
		service := strings.TrimSpace(line)
		if service == "" {
			continue
		}
		services = append(services, service)
	}
	return dedupeStringValues(services)
}

func dockerComposeOrphanServices(actual, expected []string) []string {
	if len(actual) == 0 || len(expected) == 0 {
		return nil
	}
	expectedSet := map[string]struct{}{}
	for _, service := range expected {
		expectedSet[service] = struct{}{}
	}
	var out []string
	for _, service := range dedupeStringValues(actual) {
		if _, ok := expectedSet[service]; !ok {
			out = append(out, service)
		}
	}
	return out
}

func dockerComposePriorProject(step Step) string {
	if step.Prior == nil {
		return ""
	}
	return stringMapValue(step.Prior.Desired, "project")
}

func dockerComposeDriftSummary(project, want, actual string, orphanCount int) string {
	summary := "converge docker compose project " + project + " from " + actual + " to " + want
	if orphanCount > 0 {
		summary += " and inspect " + pluralize(orphanCount, "orphan service")
	}
	return summary
}

func dockerComposeOrphanSummary(project string, orphanCount int, removeOrphans bool) string {
	if removeOrphans {
		return "remove " + pluralize(orphanCount, "orphan service") + " from docker compose project " + project
	}
	return "docker compose project " + project + " has " + pluralize(orphanCount, "orphan service") + "; set remove_orphans = true to clean them"
}

func pluralize(count int, singular string) string {
	if count == 1 {
		return "1 " + singular
	}
	return fmt.Sprintf("%d %ss", count, singular)
}

func dedupeStringValues(values []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
