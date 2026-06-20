package ir

type Program struct {
	Hosts []HostSpec `json:"hosts"`
}

type HostSpec struct {
	Name        string        `json:"name"`
	Source      SourceRef     `json:"source"`
	SSH         SSHSpec       `json:"ssh"`
	State       StateSpec     `json:"state"`
	System      SystemSpec    `json:"system"`
	Kernel      KernelSpec    `json:"kernel"`
	Packages    PackageSpec   `json:"packages"`
	APT         APTSpec       `json:"apt"`
	Files       FileSpec      `json:"files"`
	Secrets     SecretSpec    `json:"secrets"`
	Directories DirectorySpec `json:"directories"`
	Groups      GroupSpec     `json:"groups"`
	Users       UserSpec      `json:"users"`
	Systemd     SystemdSpec   `json:"systemd"`
	Services    ServiceSpec   `json:"services"`
}

type SourceRef struct {
	File string `json:"file,omitempty"`
	Line int    `json:"line,omitempty"`
	Path string `json:"path,omitempty"`
}

type LifecycleSpec struct {
	PreventDestroy bool      `json:"prevent_destroy,omitempty"`
	Source         SourceRef `json:"source,omitempty"`
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
	Name         string         `json:"name"`
	Repositories []string       `json:"repositories,omitempty"`
	Lifecycle    *LifecycleSpec `json:"lifecycle,omitempty"`
	Source       SourceRef      `json:"source,omitempty"`
}

type APTSpec struct {
	Repositories map[string]APTRepositorySpec `json:"repositories,omitempty"`
	Source       SourceRef                    `json:"source,omitempty"`
}

type APTRepositorySpec struct {
	Name          string             `json:"name"`
	URIs          []string           `json:"uris"`
	Suites        []string           `json:"suites"`
	Components    []string           `json:"components"`
	Architectures []string           `json:"architectures,omitempty"`
	SigningKey    *APTSigningKeySpec `json:"signing_key,omitempty"`
	Ensure        string             `json:"ensure"`
	Lifecycle     *LifecycleSpec     `json:"lifecycle,omitempty"`
	Source        SourceRef          `json:"source,omitempty"`
}

type APTSigningKeySpec struct {
	URL     string    `json:"url,omitempty"`
	Content string    `json:"content,omitempty"`
	SHA256  string    `json:"sha256,omitempty"`
	Path    string    `json:"path"`
	Source  SourceRef `json:"source,omitempty"`
}

type FileSpec struct {
	Files  map[string]ManagedFile `json:"files,omitempty"`
	Source SourceRef              `json:"source,omitempty"`
}

type ManagedFile struct {
	Path       string         `json:"path"`
	Content    string         `json:"content,omitempty"`
	SourcePath string         `json:"source_path,omitempty"`
	Owner      string         `json:"owner"`
	Group      string         `json:"group"`
	Mode       string         `json:"mode"`
	Sensitive  bool           `json:"sensitive,omitempty"`
	Ensure     string         `json:"ensure"`
	Lifecycle  *LifecycleSpec `json:"lifecycle,omitempty"`
	Summary    ContentSummary `json:"summary,omitempty"`
	Source     SourceRef      `json:"source,omitempty"`
}

type SecretSpec struct {
	Files  map[string]SecretFile `json:"files,omitempty"`
	Source SourceRef             `json:"source,omitempty"`
}

type SecretFile struct {
	Path       string         `json:"path"`
	SourcePath string         `json:"source_path"`
	Owner      string         `json:"owner"`
	Group      string         `json:"group"`
	Mode       string         `json:"mode"`
	Ensure     string         `json:"ensure"`
	Lifecycle  *LifecycleSpec `json:"lifecycle,omitempty"`
	Summary    ContentSummary `json:"summary,omitempty"`
	Source     SourceRef      `json:"source,omitempty"`
}

type DirectorySpec struct {
	Directories map[string]ManagedDirectory `json:"directories,omitempty"`
	Source      SourceRef                   `json:"source,omitempty"`
}

type ManagedDirectory struct {
	Path      string         `json:"path"`
	Owner     string         `json:"owner"`
	Group     string         `json:"group"`
	Mode      string         `json:"mode"`
	Ensure    string         `json:"ensure"`
	Lifecycle *LifecycleSpec `json:"lifecycle,omitempty"`
	Source    SourceRef      `json:"source,omitempty"`
}

type GroupSpec struct {
	Groups map[string]ManagedGroup `json:"groups,omitempty"`
	Source SourceRef               `json:"source,omitempty"`
}

type ManagedGroup struct {
	Name      string         `json:"name"`
	GID       string         `json:"gid,omitempty"`
	System    bool           `json:"system,omitempty"`
	Ensure    string         `json:"ensure"`
	Lifecycle *LifecycleSpec `json:"lifecycle,omitempty"`
	Source    SourceRef      `json:"source,omitempty"`
}

type UserSpec struct {
	Users  map[string]ManagedUser `json:"users,omitempty"`
	Source SourceRef              `json:"source,omitempty"`
}

type ManagedUser struct {
	Name              string         `json:"name"`
	UID               string         `json:"uid,omitempty"`
	PrimaryGroup      string         `json:"group,omitempty"`
	Groups            []string       `json:"groups,omitempty"`
	System            bool           `json:"system,omitempty"`
	Home              string         `json:"home,omitempty"`
	Shell             string         `json:"shell,omitempty"`
	SSHAuthorizedKeys []string       `json:"ssh_authorized_keys,omitempty"`
	Ensure            string         `json:"ensure"`
	Lifecycle         *LifecycleSpec `json:"lifecycle,omitempty"`
	Source            SourceRef      `json:"source,omitempty"`
}

type SystemdSpec struct {
	Units  map[string]SystemdUnit `json:"units,omitempty"`
	Source SourceRef              `json:"source,omitempty"`
}

type SystemdUnit struct {
	Name       string         `json:"name"`
	Path       string         `json:"path"`
	Content    string         `json:"content,omitempty"`
	SourcePath string         `json:"source_path,omitempty"`
	Owner      string         `json:"owner"`
	Group      string         `json:"group"`
	Mode       string         `json:"mode"`
	Ensure     string         `json:"ensure"`
	Lifecycle  *LifecycleSpec `json:"lifecycle,omitempty"`
	Summary    ContentSummary `json:"summary,omitempty"`
	Source     SourceRef      `json:"source,omitempty"`
}

type ServiceSpec struct {
	Services map[string]ManagedService `json:"services,omitempty"`
	Source   SourceRef                 `json:"source,omitempty"`
}

type ManagedService struct {
	Name      string         `json:"name"`
	Unit      string         `json:"unit"`
	Package   string         `json:"package,omitempty"`
	Enabled   *bool          `json:"enabled,omitempty"`
	State     string         `json:"state,omitempty"`
	Lifecycle *LifecycleSpec `json:"lifecycle,omitempty"`
	Source    SourceRef      `json:"source,omitempty"`
}

type ContentSummary struct {
	SHA256 string `json:"sha256,omitempty"`
	Bytes  int64  `json:"bytes,omitempty"`
}
