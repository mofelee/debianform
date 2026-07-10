package merge

import (
	"fmt"

	"github.com/mofelee/debianform/internal/core/parser"
)

func (c *compiler) resolveProfile(name string, stack []string) (resolvedProfile, error) {
	if cached, ok := c.profileCache[name]; ok {
		return cached, nil
	}
	profile, ok := c.cfg.Profiles[name]
	if !ok {
		return resolvedProfile{}, fmt.Errorf("unknown profile.%s", name)
	}
	if c.visiting[name] {
		cycle := append(append([]string(nil), stack...), name)
		return resolvedProfile{}, fmt.Errorf("%s:%d:%s: profile import cycle: %s", profile.Source.File, profile.Source.Line, profile.Source.Path, cycleString(cycle))
	}

	c.visiting[name] = true
	nextStack := append(append([]string(nil), stack...), name)
	raw := parser.MapValue(nil, profile.Source)
	asserts := []parser.Assert{}
	for _, importName := range profile.Imports {
		imported, err := c.resolveProfile(importName, nextStack)
		if err != nil {
			return resolvedProfile{}, err
		}
		merged, err := Merge(raw, imported.Body)
		if err != nil {
			return resolvedProfile{}, fmt.Errorf("%s:%d:%s: profile.%s import profile.%s: %w", profile.Source.File, profile.Source.Line, profile.Source.Path, name, importName, err)
		}
		raw = merged
		asserts = append(asserts, imported.Asserts...)
	}
	merged, err := Merge(raw, profile.Body)
	if err != nil {
		return resolvedProfile{}, fmt.Errorf("%s:%d:%s: profile.%s: %w", profile.Source.File, profile.Source.Line, profile.Source.Path, name, err)
	}
	asserts = append(asserts, profile.Asserts...)
	delete(c.visiting, name)
	resolved := resolvedProfile{Body: merged, Asserts: asserts}
	c.profileCache[name] = resolved
	return resolved, nil
}
