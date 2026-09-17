package merge

import (
	"fmt"

	"github.com/mofelee/debianform/internal/core/ir"
	"github.com/mofelee/debianform/internal/core/parser"
)

func serviceSpecs(services parser.Value) (map[string]ir.ManagedService, error) {
	objects, ok, err := objectCollection(services, "service")
	if err != nil || !ok {
		return map[string]ir.ManagedService{}, err
	}
	out := make(map[string]ir.ManagedService, len(objects))
	for _, label := range sortedKeys(objects) {
		item := objects[label]
		if label == "" {
			return nil, fmt.Errorf("%s:%d:%s: service name must be non-empty", item.Source.File, item.Source.Line, item.Source.Path)
		}
		name, err := objectName(item, "name", label)
		if err != nil {
			return nil, err
		}
		pkg, _, err := stringField(item, "package")
		if err != nil {
			return nil, err
		}
		enabledValue, hasEnabled, err := boolField(item, "enabled")
		if err != nil {
			return nil, err
		}
		var enabled *bool
		if hasEnabled {
			enabled = &enabledValue
		}
		state, _, err := stringField(item, "state")
		if err != nil {
			return nil, err
		}
		if state != "" && !validServiceState(state) {
			return nil, fmt.Errorf("%s:%d:%s.state: service state must be running, stopped, restarted, or reloaded", item.Source.File, item.Source.Line, item.Source.Path)
		}
		lifecycle, err := lifecycleSpec(item)
		if err != nil {
			return nil, err
		}
		managed := ir.ManagedService{Name: name, Unit: serviceUnitName(name), Package: pkg, Enabled: enabled, State: state, Lifecycle: lifecycle, Source: item.Source}
		if previous, exists := out[name]; exists {
			return nil, fmt.Errorf("%s:%d:%s: service %q conflicts with service declared at %s:%d:%s", item.Source.File, item.Source.Line, item.Source.Path, name, previous.Source.File, previous.Source.Line, previous.Source.Path)
		}
		out[name] = managed
	}
	return out, nil
}
