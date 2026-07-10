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
	for _, name := range sortedKeys(objects) {
		item := objects[name]
		if name == "" {
			return nil, fmt.Errorf("%s:%d:%s: service name must be non-empty", item.Source.File, item.Source.Line, item.Source.Path)
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
		out[name] = ir.ManagedService{Name: name, Unit: serviceUnitName(name), Package: pkg, Enabled: enabled, State: state, Lifecycle: lifecycle, Source: item.Source}
	}
	return out, nil
}
