package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/mofelee/debianform/internal/core/graph"
	"github.com/mofelee/debianform/internal/core/ir"
	coreplan "github.com/mofelee/debianform/internal/core/plan"
	corestate "github.com/mofelee/debianform/internal/core/state"
)

func TestMovedStatePlanCheckApplyAndBlockRemoval(t *testing.T) {
	host, resourceGraph, initial := movedNoOpFixture("server1")
	backend := newApprovalTrackingBackend()
	if _, err := backend.Write(context.Background(), host, initial); err != nil {
		t.Fatal(err)
	}
	backend.resetWrites()
	provider := NewMemoryProvider()
	engine := Engine{Backend: backend, Provider: provider}
	program := &ir.Program{Hosts: []ir.HostSpec{host}}

	preview, err := engine.Plan(context.Background(), program, resourceGraph, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Moves) != 2 || len(preview.Steps) != 0 || len(preview.Operations) != 0 {
		t.Fatalf("preview = %#v, want two moves and no remote actions", preview)
	}
	if preview.Summary.Move != 2 || preview.Summary.Create != 0 || preview.Summary.Update != 0 || preview.Summary.Delete != 0 {
		t.Fatalf("move-only summary = %#v", preview.Summary)
	}
	doc := preview.Document(coreplan.Options{})
	if doc.Summary.Move != 2 || len(doc.Moves) != 2 || doc.Moves[0].Host != "server1" || len(doc.Changes) != 0 || len(doc.Operations) != 0 {
		t.Fatalf("move-only plan document = %#v", doc)
	}
	if writes := backend.writeCount(); writes != 0 {
		t.Fatalf("plan state writes = %d, want 0", writes)
	}

	checked, err := engine.Check(context.Background(), program, resourceGraph, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(checked.Moves) != 2 || len(checked.Steps) != 0 || len(checked.Operations) != 0 {
		t.Fatalf("check plan = %#v, want pending move drift only", checked)
	}
	if writes := backend.writeCount(); writes != 0 {
		t.Fatalf("check state writes = %d, want 0", writes)
	}

	approvalCalls := 0
	applied, err := engine.Apply(context.Background(), program, resourceGraph, Options{BeforeExecute: func(_ context.Context, plan Plan) error {
		approvalCalls++
		if !backend.isLocked(host.Name) {
			t.Fatal("move approval ran without the host lock")
		}
		if backend.writeCount() != 0 {
			t.Fatalf("state was written before move approval")
		}
		if len(plan.Moves) != 2 || len(plan.Steps) != 0 || len(plan.Operations) != 0 {
			t.Fatalf("locked plan = %#v", plan)
		}
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	if approvalCalls != 1 || len(applied.Moves) != 2 {
		t.Fatalf("approval calls = %d plan = %#v", approvalCalls, applied)
	}
	if writes := backend.writeCount(); writes != 1 {
		t.Fatalf("move-only writes = %d, want one atomic host write", writes)
	}
	if len(provider.Applied) != 0 || len(provider.Destroyed) != 0 || len(provider.Operations) != 0 {
		t.Fatalf("move-only provider mutations: applied=%#v destroyed=%#v operations=%#v", provider.Applied, provider.Destroyed, provider.Operations)
	}
	st, err := backend.Read(context.Background(), host)
	if err != nil {
		t.Fatal(err)
	}
	if st.Serial != 2 {
		t.Fatalf("move-only serial = %d, want 2 after one seeded and one migration write", st.Serial)
	}
	for _, node := range resourceGraph.Nodes {
		resource, ok := st.Resources[node.Address]
		if !ok {
			t.Fatalf("destination state missing %s: %#v", node.Address, st.Resources)
		}
		if resource.Desired["component"] != "current" || resource.ProviderAddress != node.ProviderAddress {
			t.Fatalf("destination resource was not rebased to graph node: %#v", resource)
		}
	}

	retained, err := engine.Plan(context.Background(), program, resourceGraph, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(retained.Moves) != 0 || len(retained.Steps) != 0 || len(retained.Operations) != 0 {
		t.Fatalf("retained moved block did not converge: %#v", retained)
	}
	program.Hosts[0].Moves = nil
	removed, err := engine.Check(context.Background(), program, resourceGraph, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(removed.Moves) != 0 || len(removed.Steps) != 0 || len(removed.Operations) != 0 {
		t.Fatalf("removed moved block reversed migration: %#v", removed)
	}
}

func TestMovedStateWriteFailureAbortsProviderAndRetryIsSafe(t *testing.T) {
	host, node, initial := movedUpdateFixture("server1")
	backend := newScriptedCommitBackend(initial, 1)
	provider := NewMemoryProvider()
	engine := Engine{Backend: backend, Provider: provider}
	program := &ir.Program{Hosts: []ir.HostSpec{host}}
	resourceGraph := &graph.ResourceGraph{Nodes: []graph.Node{node}}

	plan, err := engine.Apply(context.Background(), program, resourceGraph, Options{})
	if !errors.Is(err, errInjectedStateWrite) {
		t.Fatalf("apply error = %v, want migration write failure", err)
	}
	if len(plan.Moves) != 1 || len(plan.Steps) != 1 || plan.Steps[0].Action != ActionUpdate {
		t.Fatalf("failed apply plan = %#v, want move plus real update", plan)
	}
	if len(provider.Applied) != 0 || len(provider.Destroyed) != 0 || len(provider.Operations) != 0 {
		t.Fatalf("provider mutation began after failed move write")
	}
	failed, err := backend.Read(context.Background(), host)
	if err != nil {
		t.Fatal(err)
	}
	if failed.Serial != 7 || !hasAddressPrefix(failed, "host.server1.components.old") {
		t.Fatalf("failed migration changed durable state: %#v", failed)
	}

	retried, err := engine.Apply(context.Background(), program, resourceGraph, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(retried.Moves) != 1 || len(provider.Applied) != 1 || provider.Applied[0] != node.Address {
		t.Fatalf("retry plan/provider = %#v / %#v", retried, provider.Applied)
	}
	committed, err := backend.Read(context.Background(), host)
	if err != nil {
		t.Fatal(err)
	}
	if committed.Serial != 9 || hasAddressPrefix(committed, "host.server1.components.old") {
		t.Fatalf("retry state = %#v, want move serial 8 plus resource serial 9", committed)
	}
}

func TestMovedStateRemainsCommittedAfterLaterProviderFailure(t *testing.T) {
	host, node, initial := movedUpdateFixture("server1")
	backend := newScriptedCommitBackend(initial, 0)
	provider := NewMemoryProvider()
	provider.FailApplyAt = node.Address
	engine := Engine{Backend: backend, Provider: provider}
	program := &ir.Program{Hosts: []ir.HostSpec{host}}
	resourceGraph := &graph.ResourceGraph{Nodes: []graph.Node{node}}

	first, err := engine.Apply(context.Background(), program, resourceGraph, Options{})
	if err == nil || !strings.Contains(err.Error(), "injected failure") {
		t.Fatalf("apply error = %v, want provider failure", err)
	}
	if len(first.Moves) != 1 || len(first.Steps) != 1 {
		t.Fatalf("first plan = %#v", first)
	}
	afterFailure, err := backend.Read(context.Background(), host)
	if err != nil {
		t.Fatal(err)
	}
	if afterFailure.Serial != 8 || hasAddressPrefix(afterFailure, "host.server1.components.old") || !hasAddressPrefix(afterFailure, "host.server1.components.current") {
		t.Fatalf("move was not retained after provider failure: %#v", afterFailure)
	}

	provider.FailApplyAt = ""
	retry, err := engine.Apply(context.Background(), program, resourceGraph, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(retry.Moves) != 0 || len(retry.Steps) != 1 || retry.Steps[0].Action != ActionUpdate {
		t.Fatalf("retry plan = %#v, want only the real update", retry)
	}
	final, err := backend.Read(context.Background(), host)
	if err != nil {
		t.Fatal(err)
	}
	if final.Serial != 9 {
		t.Fatalf("retry serial = %d, want one move write and one successful resource write", final.Serial)
	}
}

func TestMovedStatePartialMultiHostRetryAndHostFilter(t *testing.T) {
	host1, graph1, initial1 := movedNoOpFixture("server1")
	host2, graph2, initial2 := movedNoOpFixture("server2")
	resourceGraph := &graph.ResourceGraph{
		Nodes:      append(append([]graph.Node(nil), graph1.Nodes...), graph2.Nodes...),
		Operations: append(append([]graph.Operation(nil), graph1.Operations...), graph2.Operations...),
	}
	backend := newHostWriteFailureBackend(map[string]corestate.State{"server1": initial1, "server2": initial2}, "server2")
	provider := NewMemoryProvider()
	engine := Engine{Backend: backend, Provider: provider}
	program := &ir.Program{Hosts: []ir.HostSpec{host1, host2}}

	filtered, err := engine.Plan(context.Background(), program, resourceGraph, Options{Host: "server2"})
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered.Moves) != 2 || filtered.Moves[0].Host != "server2" {
		t.Fatalf("host-filtered moves = %#v", filtered.Moves)
	}

	_, err = engine.Apply(context.Background(), program, resourceGraph, Options{Parallel: 1})
	if !errors.Is(err, errInjectedHostMoveWrite) {
		t.Fatalf("first multi-host apply error = %v", err)
	}
	if len(provider.Applied) != 0 || len(provider.Operations) != 0 {
		t.Fatalf("provider mutation began before every host move write succeeded")
	}
	firstState, _ := backend.Read(context.Background(), host1)
	secondState, _ := backend.Read(context.Background(), host2)
	if hasAddressPrefix(firstState, "host.server1.components.old") || !hasAddressPrefix(secondState, "host.server2.components.old") {
		t.Fatalf("partial states = server1:%#v server2:%#v", firstState.Resources, secondState.Resources)
	}

	retryPlan, err := engine.Apply(context.Background(), program, resourceGraph, Options{Parallel: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(retryPlan.Moves) != 2 || retryPlan.Moves[0].Host != "server2" {
		t.Fatalf("retry moves = %#v, want only server2 leaf moves", retryPlan.Moves)
	}
	secondState, _ = backend.Read(context.Background(), host2)
	if hasAddressPrefix(secondState, "host.server2.components.old") {
		t.Fatalf("server2 did not migrate on retry: %#v", secondState.Resources)
	}
}

func TestMovedStateMystackFiveHostAcceptance(t *testing.T) {
	hostNames := []string{"rfchk", "rflon", "rfsea", "rfsgp", "rfsyd"}
	backend := NewMemoryBackend()
	provider := NewMemoryProvider()
	program := &ir.Program{}
	resourceGraph := &graph.ResourceGraph{}

	for _, hostName := range hostNames {
		host, nodes, operation, initial := mystackMovedFixture(hostName)
		program.Hosts = append(program.Hosts, host)
		resourceGraph.Nodes = append(resourceGraph.Nodes, nodes...)
		resourceGraph.Operations = append(resourceGraph.Operations, operation)
		if _, err := backend.Write(context.Background(), host, initial); err != nil {
			t.Fatal(err)
		}
	}

	engine := Engine{Backend: backend, Provider: provider}
	preview, err := engine.Plan(context.Background(), program, resourceGraph, Options{Parallel: len(hostNames)})
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Moves) != 35 || len(preview.Steps) != 5 || len(preview.Operations) != 5 {
		t.Fatalf("five-host preview = %d moves, %d steps, %d operations; want 35, 5, 5", len(preview.Moves), len(preview.Steps), len(preview.Operations))
	}
	if preview.Summary.Move != 35 || preview.Summary.Create != 0 || preview.Summary.Update != 5 || preview.Summary.Delete != 0 || preview.Summary.Operations != 5 {
		t.Fatalf("five-host summary = %#v", preview.Summary)
	}

	wantHosts := make(map[string]bool, len(hostNames))
	for _, hostName := range hostNames {
		wantHosts[hostName] = true
	}
	for _, step := range preview.Steps {
		wantAddress := fmt.Sprintf(`host.%s.components.bird2_ospfv3.files.file["/etc/bird/bird.conf"]`, step.Host)
		if step.Action != ActionUpdate || step.Address != wantAddress {
			t.Fatalf("rename introduced a false remote action: %#v", step)
		}
		delete(wantHosts, step.Host)
	}
	if len(wantHosts) != 0 {
		t.Fatalf("hosts missing the one real file update: %#v", wantHosts)
	}
	for _, operation := range preview.Operations {
		wantAddress := fmt.Sprintf(`host.%s.components.bird2_ospfv3.script["reload_bird"]`, operation.Operation.Host)
		wantTrigger := fmt.Sprintf(`host.%s.components.bird2_ospfv3.files.file["/etc/bird/bird.conf"]`, operation.Operation.Host)
		if operation.Address != wantAddress || len(operation.Operation.TriggeredBy) != 1 || operation.Operation.TriggeredBy[0] != wantTrigger {
			t.Fatalf("unexpected rename operation: %#v", operation)
		}
	}

	applied, err := engine.Apply(context.Background(), program, resourceGraph, Options{Parallel: len(hostNames)})
	if err != nil {
		t.Fatal(err)
	}
	if len(applied.Moves) != 35 || len(provider.Applied) != 5 || len(provider.Destroyed) != 0 || len(provider.Operations) != 5 {
		t.Fatalf("five-host apply caused false provider work: plan=%#v applied=%#v destroyed=%#v operations=%#v", applied, provider.Applied, provider.Destroyed, provider.Operations)
	}

	retained, err := engine.Check(context.Background(), program, resourceGraph, Options{Parallel: len(hostNames)})
	if err != nil {
		t.Fatal(err)
	}
	if len(retained.Moves) != 0 || len(retained.Steps) != 0 || len(retained.Operations) != 0 {
		t.Fatalf("retained five-host moved block did not converge: %#v", retained)
	}
	for i := range program.Hosts {
		program.Hosts[i].Moves = nil
	}
	removed, err := engine.Check(context.Background(), program, resourceGraph, Options{Parallel: len(hostNames)})
	if err != nil {
		t.Fatal(err)
	}
	if len(removed.Moves) != 0 || len(removed.Steps) != 0 || len(removed.Operations) != 0 {
		t.Fatalf("removed five-host moved block did not stay converged: %#v", removed)
	}
}

func TestMovedStateLockLossBeforePersistenceLeavesStateUntouched(t *testing.T) {
	host, resourceGraph, initial := movedNoOpFixture("server1")
	leaseCause := errors.New("injected move lease loss")
	lease := &testRenewableLock{lost: make(chan struct{})}
	memory := NewMemoryBackend()
	if _, err := memory.Write(context.Background(), host, initial); err != nil {
		t.Fatal(err)
	}
	backend := &testRenewableBackend{MemoryBackend: memory, lock: lease}
	provider := NewMemoryProvider()
	engine := Engine{Backend: backend, Provider: provider}

	plan, err := engine.Apply(context.Background(), &ir.Program{Hosts: []ir.HostSpec{host}}, resourceGraph, Options{BeforeExecute: func(ctx context.Context, plan Plan) error {
		if len(plan.Moves) == 0 {
			t.Fatal("locked plan omitted pending move")
		}
		lease.fail(leaseCause)
		<-ctx.Done()
		return nil
	}})
	if !errors.Is(err, leaseCause) {
		t.Fatalf("apply error = %v, want lease cause", err)
	}
	if len(plan.Moves) != 2 || len(provider.Applied) != 0 {
		t.Fatalf("lease-loss plan/provider = %#v / %#v", plan, provider.Applied)
	}
	st, readErr := backend.Read(context.Background(), host)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !hasAddressPrefix(st, "host.server1.components.old") || hasAddressPrefix(st, "host.server1.components.current") {
		t.Fatalf("state changed after lock loss: %#v", st.Resources)
	}
}

func movedNoOpFixture(hostName string) (ir.HostSpec, *graph.ResourceGraph, corestate.State) {
	oldPrefix := fmt.Sprintf("host.%s.components.old", hostName)
	newPrefix := fmt.Sprintf("host.%s.components.current", hostName)
	fileAddress := newPrefix + `.files.file["/etc/example.conf"]`
	outputAddress := newPrefix + `.script["reload"].outputs["/var/lib/example.out"]`
	fileDesired := map[string]any{"component": "current", "path": "/etc/example.conf", "content": "same", "owner": "root", "group": "root", "mode": "0644", "ensure": "present"}
	outputDesired := map[string]any{"component": "current", "path": "/var/lib/example.out", "script": "reload", "script_digest": "same-script"}
	fileNode := graph.Node{Host: hostName, Address: fileAddress, Kind: "file", Summary: "manage example", Desired: fileDesired, ProviderType: "file", ProviderAddress: "file." + hostName + "_current_example"}
	outputNode := graph.Node{Host: hostName, Address: outputAddress, Kind: "component_script_output", Summary: "check output", Desired: outputDesired, ProviderType: "component_script_output", ProviderAddress: "component_script_output." + hostName + "_current_reload"}
	operation := graph.Operation{
		Host:        hostName,
		Address:     newPrefix + `.script["reload"]`,
		Action:      ActionRun,
		Summary:     "reload example",
		DependsOn:   []string{fileAddress, outputAddress},
		TriggeredBy: []string{fileAddress, outputAddress},
		ScriptPayload: &graph.ScriptPayload{
			Name:          "reload",
			ComponentName: "current",
			Outputs: []graph.ScriptOutputPayload{{
				Address:         outputAddress,
				Path:            "/var/lib/example.out",
				ScriptDigest:    "same-script",
				ProviderAddress: outputNode.ProviderAddress,
			}},
		},
	}
	host := ir.HostSpec{
		Name:       hostName,
		Components: []ir.ComponentInstanceSpec{{Name: "current"}},
		Moves: []ir.MovedSpec{{
			From:       oldPrefix,
			To:         newPrefix,
			FromSource: ir.SourceRef{File: "main.dbf.hcl", Line: 2, Path: "moved.from"},
			ToSource:   ir.SourceRef{File: "main.dbf.hcl", Line: 3, Path: "moved.to"},
		}},
	}
	initial := corestate.Empty(hostName)
	initial.Resources[strings.Replace(fileAddress, newPrefix, oldPrefix, 1)] = movedPriorResource(fileNode, "current", "old")
	initial.Resources[strings.Replace(outputAddress, newPrefix, oldPrefix, 1)] = movedPriorResource(outputNode, "current", "old")
	return host, &graph.ResourceGraph{Nodes: []graph.Node{fileNode, outputNode}, Operations: []graph.Operation{operation}}, initial
}

func movedUpdateFixture(hostName string) (ir.HostSpec, graph.Node, corestate.State) {
	oldPrefix := fmt.Sprintf("host.%s.components.old", hostName)
	newPrefix := fmt.Sprintf("host.%s.components.current", hostName)
	address := newPrefix + `.files.file["/etc/example.conf"]`
	node := graph.Node{
		Host:            hostName,
		Address:         address,
		Kind:            "file",
		Summary:         "update example",
		Desired:         map[string]any{"component": "current", "path": "/etc/example.conf", "content": "new", "owner": "root", "group": "root", "mode": "0644", "ensure": "present"},
		ProviderType:    "file",
		ProviderAddress: "file." + hostName + "_current_example",
	}
	priorNode := node
	priorNode.Desired = cloneMap(node.Desired)
	priorNode.Desired["content"] = "old"
	host := ir.HostSpec{
		Name:       hostName,
		Components: []ir.ComponentInstanceSpec{{Name: "current"}},
		Moves: []ir.MovedSpec{{
			From:       oldPrefix,
			To:         newPrefix,
			FromSource: ir.SourceRef{File: "main.dbf.hcl", Line: 2, Path: "moved.from"},
			ToSource:   ir.SourceRef{File: "main.dbf.hcl", Line: 3, Path: "moved.to"},
		}},
	}
	initial := corestate.Empty(hostName)
	initial.Serial = 7
	initial.Resources[strings.Replace(address, newPrefix, oldPrefix, 1)] = movedPriorResource(priorNode, "current", "old")
	return host, node, initial
}

func mystackMovedFixture(hostName string) (ir.HostSpec, []graph.Node, graph.Operation, corestate.State) {
	oldPrefix := fmt.Sprintf("host.%s.components.bird2_babel", hostName)
	newPrefix := fmt.Sprintf("host.%s.components.bird2_ospfv3", hostName)
	configAddress := newPrefix + `.files.file["/etc/bird/bird.conf"]`
	outputAddress := newPrefix + `.script["reload_bird"].outputs["/var/lib/bird/reload.stamp"]`
	nodes := []graph.Node{
		{
			Host: hostName, Address: newPrefix + `.apt.signing_key["bird2"]`, Kind: "apt_signing_key", Summary: "manage BIRD signing key",
			Desired:      map[string]any{"component": "bird2_ospfv3", "path": "/etc/apt/keyrings/bird2.asc", "url": "https://packages.example.invalid/bird2.asc", "sha256": "same-key"},
			ProviderType: "apt_signing_key", ProviderAddress: "apt_signing_key." + hostName + "_bird2_ospfv3",
		},
		{
			Host: hostName, Address: newPrefix + `.apt.repository["bird2"]`, Kind: "apt_repository", Summary: "manage BIRD repository",
			Desired:      map[string]any{"component": "bird2_ospfv3", "name": "bird2", "uris": []string{"https://packages.example.invalid/bird2"}, "suites": []string{"trixie"}},
			ProviderType: "apt_repository", ProviderAddress: "apt_repository." + hostName + "_bird2_ospfv3", DependsOn: []string{newPrefix + `.apt.signing_key["bird2"]`},
		},
		{
			Host: hostName, Address: newPrefix + `.packages.install["bird2"]`, Kind: "package", Summary: "install bird2",
			Desired:      map[string]any{"component": "bird2_ospfv3", "name": "bird2", "ensure": "present"},
			ProviderType: "package", ProviderAddress: "package." + hostName + "_bird2_ospfv3", DependsOn: []string{newPrefix + `.apt.repository["bird2"]`},
		},
		{
			Host: hostName, Address: newPrefix + `.services.service["bird"]`, Kind: "service", Summary: "keep bird running",
			Desired:      map[string]any{"component": "bird2_ospfv3", "name": "bird", "unit": "bird.service", "enabled": true, "state": "running"},
			ProviderType: "service", ProviderAddress: "service." + hostName + "_bird2_ospfv3", DependsOn: []string{newPrefix + `.packages.install["bird2"]`, configAddress},
		},
		{
			Host: hostName, Address: newPrefix + `.files.file["/etc/bird/bird.env"]`, Kind: "file", Summary: "manage unchanged BIRD environment",
			Desired:      map[string]any{"component": "bird2_ospfv3", "path": "/etc/bird/bird.env", "content": "BIRD_ARGS=-f\n", "owner": "root", "group": "root", "mode": "0644", "ensure": "present"},
			ProviderType: "file", ProviderAddress: "file." + hostName + "_bird2_ospfv3_env", Lifecycle: &ir.LifecycleSpec{PreventDestroy: true},
		},
		{
			Host: hostName, Address: configAddress, Kind: "file", Summary: "switch BIRD from Babel standby to OSPFv3 only",
			Desired:      map[string]any{"component": "bird2_ospfv3", "path": "/etc/bird/bird.conf", "content": "protocol ospf v3 edge {}\n", "owner": "root", "group": "root", "mode": "0644", "ensure": "present"},
			ProviderType: "file", ProviderAddress: "file." + hostName + "_bird2_ospfv3_conf",
		},
		{
			Host: hostName, Address: outputAddress, Kind: "component_script_output", Summary: "observe BIRD reload output",
			Desired:      map[string]any{"component": "bird2_ospfv3", "path": "/var/lib/bird/reload.stamp", "script": "reload_bird", "script_digest": "same-script"},
			ProviderType: "component_script_output", ProviderAddress: "component_script_output." + hostName + "_bird2_ospfv3_reload",
		},
	}

	initial := corestate.Empty(hostName)
	for _, node := range nodes {
		priorNode := node
		priorNode.Desired = cloneMap(node.Desired)
		if node.Address == configAddress {
			priorNode.Desired["content"] = "protocol ospf v3 edge {}\nprotocol babel edge {}\n"
		}
		oldAddress := strings.Replace(node.Address, newPrefix, oldPrefix, 1)
		resource := movedPriorResource(priorNode, "bird2_ospfv3", "bird2_babel")
		switch node.Kind {
		case "package", "component_script_output":
			resource.Ownership = "adopted"
		case "service":
			resource.Ownership = "external"
		}
		resource.Lifecycle = cloneLifecycle(node.Lifecycle)
		initial.Resources[oldAddress] = resource
	}

	host := ir.HostSpec{
		Name:       hostName,
		Components: []ir.ComponentInstanceSpec{{Name: "bird2_ospfv3"}},
		Moves: []ir.MovedSpec{{
			From:       oldPrefix,
			To:         newPrefix,
			FromSource: ir.SourceRef{File: "mystack.dbf.hcl", Line: 2, Path: "moved.from"},
			ToSource:   ir.SourceRef{File: "mystack.dbf.hcl", Line: 3, Path: "moved.to"},
		}},
	}
	operation := graph.Operation{
		Host: hostName, Address: newPrefix + `.script["reload_bird"]`, Action: ActionRun, Summary: "reload BIRD after the real configuration update",
		DependsOn: []string{configAddress, outputAddress}, TriggeredBy: []string{configAddress},
	}
	return host, nodes, operation, initial
}

func movedPriorResource(node graph.Node, fromComponent, toComponent string) corestate.Resource {
	desired := cloneMap(node.Desired)
	desired["component"] = toComponent
	digest := corestate.DesiredDigest(desired)
	return corestate.Resource{
		Host:            node.Host,
		Kind:            node.Kind,
		ProviderType:    node.ProviderType,
		ProviderAddress: strings.ReplaceAll(node.ProviderAddress, fromComponent, toComponent),
		Ownership:       "managed",
		Desired:         desired,
		DesiredDigest:   digest,
		Observed:        map[string]any{"exists": true, "desired_digest": digest},
	}
}

func hasAddressPrefix(st corestate.State, prefix string) bool {
	for address := range st.Resources {
		if address == prefix || strings.HasPrefix(address, prefix+".") {
			return true
		}
	}
	return false
}

var errInjectedHostMoveWrite = errors.New("injected host move write failure")

type hostWriteFailureBackend struct {
	*MemoryBackend
	mu       sync.Mutex
	failHost string
	failed   bool
}

func newHostWriteFailureBackend(states map[string]corestate.State, failHost string) *hostWriteFailureBackend {
	backend := &hostWriteFailureBackend{MemoryBackend: NewMemoryBackend(), failHost: failHost}
	for host, st := range states {
		backend.MemoryBackend.states[host] = cloneState(st)
	}
	return backend
}

func (b *hostWriteFailureBackend) Write(ctx context.Context, host ir.HostSpec, st corestate.State) (corestate.State, error) {
	b.mu.Lock()
	if host.Name == b.failHost && !b.failed {
		b.failed = true
		b.mu.Unlock()
		return corestate.State{}, errInjectedHostMoveWrite
	}
	b.mu.Unlock()
	return b.MemoryBackend.Write(ctx, host, st)
}
