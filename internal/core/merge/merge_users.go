package merge

import (
	"fmt"

	"github.com/mofelee/debianform/internal/core/ir"
	"github.com/mofelee/debianform/internal/core/parser"
)

func groupSpecs(groups parser.Value) (map[string]ir.ManagedGroup, error) {
	objects, ok, err := objectCollection(groups, "group")
	if err != nil || !ok {
		return map[string]ir.ManagedGroup{}, err
	}
	out := make(map[string]ir.ManagedGroup, len(objects))
	for _, name := range sortedKeys(objects) {
		item := objects[name]
		if name == "" {
			return nil, fmt.Errorf("%s:%d:%s: group name must be non-empty", item.Source.File, item.Source.Line, item.Source.Path)
		}
		system, ok, err := boolField(item, "system")
		if err != nil {
			return nil, err
		}
		if !ok {
			system = false
		}
		gid, _, err := stringField(item, "gid")
		if err != nil {
			return nil, err
		}
		ensure, err := ensureField(item, "present")
		if err != nil {
			return nil, err
		}
		lifecycle, err := lifecycleSpec(item)
		if err != nil {
			return nil, err
		}
		out[name] = ir.ManagedGroup{Name: name, GID: gid, System: system, Ensure: ensure, Lifecycle: lifecycle, Source: item.Source}
	}
	return out, nil
}

func userSpecs(users parser.Value) (map[string]ir.ManagedUser, error) {
	objects, ok, err := objectCollection(users, "user")
	if err != nil || !ok {
		return map[string]ir.ManagedUser{}, err
	}
	out := make(map[string]ir.ManagedUser, len(objects))
	for _, name := range sortedKeys(objects) {
		item := objects[name]
		if name == "" {
			return nil, fmt.Errorf("%s:%d:%s: user name must be non-empty", item.Source.File, item.Source.Line, item.Source.Path)
		}
		system, ok, err := boolField(item, "system")
		if err != nil {
			return nil, err
		}
		if !ok {
			system = false
		}
		uid, _, err := stringField(item, "uid")
		if err != nil {
			return nil, err
		}
		home, _, err := stringField(item, "home")
		if err != nil {
			return nil, err
		}
		shell, _, err := stringField(item, "shell")
		if err != nil {
			return nil, err
		}
		primaryGroup, _, err := stringField(item, "group")
		if err != nil {
			return nil, err
		}
		groups, err := stringListField(item, "groups")
		if err != nil {
			return nil, err
		}
		keys, err := stringListField(item, "ssh_authorized_keys")
		if err != nil {
			return nil, err
		}
		ensure, err := ensureField(item, "present")
		if err != nil {
			return nil, err
		}
		lifecycle, err := lifecycleSpec(item)
		if err != nil {
			return nil, err
		}
		out[name] = ir.ManagedUser{
			Name:              name,
			UID:               uid,
			PrimaryGroup:      primaryGroup,
			Groups:            groups,
			System:            system,
			Home:              home,
			Shell:             shell,
			SSHAuthorizedKeys: keys,
			Ensure:            ensure,
			Lifecycle:         lifecycle,
			Source:            item.Source,
		}
	}
	return out, nil
}
