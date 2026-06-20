package ir

type Program struct {
	Hosts []HostSpec `json:"hosts"`
}

type HostSpec struct {
	Name     string      `json:"name"`
	Source   SourceRef   `json:"source"`
	SSH      SSHSpec     `json:"ssh"`
	State    StateSpec   `json:"state"`
	System   SystemSpec  `json:"system"`
	Kernel   KernelSpec  `json:"kernel"`
	Packages PackageSpec `json:"packages"`
}

type SourceRef struct {
	File string `json:"file,omitempty"`
	Line int    `json:"line,omitempty"`
	Path string `json:"path,omitempty"`
}

type SSHSpec struct {
	Host         string    `json:"host"`
	Port         int       `json:"port,omitempty"`
	User         string    `json:"user,omitempty"`
	IdentityFile string    `json:"identity_file,omitempty"`
	Source       SourceRef `json:"source,omitempty"`
}

type StateSpec struct {
	Path     string    `json:"path"`
	LockPath string    `json:"lock_path"`
	Source   SourceRef `json:"source,omitempty"`
}

type SystemSpec struct {
	Hostname     string    `json:"hostname"`
	Architecture string    `json:"architecture,omitempty"`
	Codename     string    `json:"codename,omitempty"`
	Timezone     string    `json:"timezone,omitempty"`
	Locale       string    `json:"locale,omitempty"`
	Source       SourceRef `json:"source,omitempty"`
}

type KernelSpec struct {
	Modules []KernelModuleSpec    `json:"modules,omitempty"`
	Sysctl  map[string]SysctlSpec `json:"sysctl,omitempty"`
	Source  SourceRef             `json:"source,omitempty"`
}

type KernelModuleSpec struct {
	Name    string    `json:"name"`
	Persist bool      `json:"persist"`
	Ensure  string    `json:"ensure"`
	Source  SourceRef `json:"source,omitempty"`
}

type SysctlSpec struct {
	Key          string    `json:"key"`
	Value        string    `json:"value"`
	Persist      bool      `json:"persist"`
	ApplyRuntime bool      `json:"apply_runtime"`
	Source       SourceRef `json:"source,omitempty"`
}

type PackageSpec struct {
	Install []PackageItem `json:"install,omitempty"`
	Source  SourceRef     `json:"source,omitempty"`
}

type PackageItem struct {
	Name   string    `json:"name"`
	Source SourceRef `json:"source,omitempty"`
}
