package ir

import "encoding/json"

type variableSpecJSON VariableSpec

func (v VariableSpec) MarshalJSON() ([]byte, error) {
	out := variableSpecJSON(v)
	if v.Sensitive && v.Default != nil {
		out.Default = "<sensitive>"
	}
	return json.Marshal(out)
}

type managedFileJSON ManagedFile

func (m ManagedFile) MarshalJSON() ([]byte, error) {
	out := managedFileJSON(m)
	if m.Sensitive {
		out.Content = ""
		out.SourcePath = ""
	}
	return json.Marshal(out)
}

type aptSigningKeySpecJSON APTSigningKeySpec

func (s APTSigningKeySpec) MarshalJSON() ([]byte, error) {
	out := aptSigningKeySpecJSON(s)
	if s.Sensitive {
		out.Content = ""
	}
	return json.Marshal(out)
}

type aptSourceFileSpecJSON APTSourceFileSpec

func (s APTSourceFileSpec) MarshalJSON() ([]byte, error) {
	out := aptSourceFileSpecJSON(s)
	if s.Sensitive {
		out.Content = ""
		out.SourcePath = ""
	}
	return json.Marshal(out)
}

type nftablesFileSpecJSON NftablesFileSpec

func (n NftablesFileSpec) MarshalJSON() ([]byte, error) {
	out := nftablesFileSpecJSON(n)
	if n.Sensitive {
		out.Content = ""
		out.SourcePath = ""
	}
	return json.Marshal(out)
}

type componentScriptSpecJSON ComponentScriptSpec

func (s ComponentScriptSpec) MarshalJSON() ([]byte, error) {
	out := componentScriptSpecJSON(s)
	if s.Sensitive {
		out.Interpreter = nil
		out.Outputs = nil
		out.Run = ""
		out.Content = ""
		out.Commands = nil
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
