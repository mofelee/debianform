package ir

import "encoding/json"

type managedFileJSON ManagedFile

func (m ManagedFile) MarshalJSON() ([]byte, error) {
	out := managedFileJSON(m)
	if m.Sensitive {
		out.Content = ""
		out.SourcePath = ""
	}
	return json.Marshal(out)
}

type systemdUnitJSON SystemdUnit

func (u SystemdUnit) MarshalJSON() ([]byte, error) {
	out := systemdUnitJSON(u)
	if u.Sensitive {
		out.Content = ""
		out.SourcePath = ""
	}
	return json.Marshal(out)
}
