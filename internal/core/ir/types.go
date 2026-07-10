package ir

type Program struct {
	Hosts      []HostSpec                       `json:"hosts"`
	Variables  map[string]VariableSpec          `json:"variables,omitempty"`
	Components map[string]ComponentTemplateSpec `json:"components,omitempty"`
}

type Warning struct {
	Source  SourceRef `json:"source,omitempty"`
	Message string    `json:"message"`
}

type HostSpec struct {
	Name        string                  `json:"name"`
	Source      SourceRef               `json:"source"`
	Facts       HostFacts               `json:"facts,omitempty"`
	SSH         SSHSpec                 `json:"ssh"`
	State       StateSpec               `json:"state"`
	Platform    *PlatformSpec           `json:"platform,omitempty"`
	System      SystemSpec              `json:"system"`
	Kernel      KernelSpec              `json:"kernel"`
	Packages    PackageSpec             `json:"packages"`
	APT         APTSpec                 `json:"apt"`
	Files       FileSpec                `json:"files"`
	Secrets     SecretSpec              `json:"secrets"`
	Directories DirectorySpec           `json:"directories"`
	Groups      GroupSpec               `json:"groups"`
	Users       UserSpec                `json:"users"`
	Systemd     SystemdSpec             `json:"systemd"`
	Services    ServiceSpec             `json:"services"`
	Nftables    NftablesSpec            `json:"nftables"`
	Docker      *DockerSpec             `json:"docker,omitempty"`
	Components  []ComponentInstanceSpec `json:"components,omitempty"`
}

func (h HostSpec) PlatformArchitecture() string {
	if h.Platform != nil && h.Platform.Architecture != "" {
		return h.Platform.Architecture
	}
	return ""
}

func (h HostSpec) PlatformCodename() string {
	if h.Platform != nil && h.Platform.Codename != "" {
		return h.Platform.Codename
	}
	return ""
}

type SourceRef struct {
	File string `json:"file,omitempty"`
	Line int    `json:"line,omitempty"`
	Path string `json:"path,omitempty"`
}

type HostFacts struct {
	System SystemFacts `json:"system,omitempty"`
}

type SystemFacts struct {
	Hostname     string `json:"hostname,omitempty"`
	Architecture string `json:"architecture,omitempty"`
	Codename     string `json:"codename,omitempty"`
	DetectedAt   string `json:"detected_at,omitempty"`
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

type PlatformSpec struct {
	Architecture string    `json:"architecture,omitempty"`
	Codename     string    `json:"codename,omitempty"`
	Source       SourceRef `json:"source,omitempty"`
}

type SystemSpec struct {
	Hostname    string    `json:"hostname,omitempty"`
	HostnameSet bool      `json:"hostname_set,omitempty"`
	Timezone    string    `json:"timezone,omitempty"`
	TimezoneSet bool      `json:"timezone_set,omitempty"`
	Locale      string    `json:"locale,omitempty"`
	LocaleSet   bool      `json:"locale_set,omitempty"`
	Source      SourceRef `json:"source,omitempty"`
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
	SourceFiles  map[string]APTSourceFileSpec `json:"source_files,omitempty"`
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
	URL       string    `json:"url,omitempty"`
	Content   string    `json:"content,omitempty"`
	SHA256    string    `json:"sha256,omitempty"`
	Path      string    `json:"path"`
	Sensitive bool      `json:"sensitive,omitempty"`
	Source    SourceRef `json:"source,omitempty"`
}

type APTSourceFileSpec struct {
	Label      string         `json:"label"`
	Path       string         `json:"path"`
	Content    string         `json:"content,omitempty"`
	SourcePath string         `json:"source_path,omitempty"`
	Owner      string         `json:"owner"`
	Group      string         `json:"group"`
	Mode       string         `json:"mode"`
	Ensure     string         `json:"ensure"`
	OnDestroy  string         `json:"on_destroy"`
	Sensitive  bool           `json:"sensitive,omitempty"`
	Lifecycle  *LifecycleSpec `json:"lifecycle,omitempty"`
	Summary    ContentSummary `json:"summary,omitempty"`
	Source     SourceRef      `json:"source,omitempty"`
}

type FileSpec struct {
	Files  map[string]ManagedFile `json:"files,omitempty"`
	Source SourceRef              `json:"source,omitempty"`
}

type ManagedFile struct {
	Path             string         `json:"path"`
	Content          string         `json:"content,omitempty"`
	ContentVersion   string         `json:"content_version,omitempty"`
	ContentWriteOnly bool           `json:"content_write_only,omitempty"`
	SourcePath       string         `json:"source_path,omitempty"`
	Owner            string         `json:"owner"`
	Group            string         `json:"group"`
	Mode             string         `json:"mode"`
	Sensitive        bool           `json:"sensitive,omitempty"`
	Ensure           string         `json:"ensure"`
	OnChange         string         `json:"on_change,omitempty"`
	Lifecycle        *LifecycleSpec `json:"lifecycle,omitempty"`
	Summary          ContentSummary `json:"summary,omitempty"`
	OnChangeSource   *SourceRef     `json:"on_change_source,omitempty"`
	Source           SourceRef      `json:"source,omitempty"`
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
	Units    map[string]SystemdUnit  `json:"units,omitempty"`
	Networkd *NetworkdSpec           `json:"networkd,omitempty"`
	Timers   map[string]SystemdTimer `json:"timers,omitempty"`
	Resolved *SystemdResolvedSpec    `json:"resolved,omitempty"`
	Journald *SystemdJournaldSpec    `json:"journald,omitempty"`
	Source   SourceRef               `json:"source,omitempty"`
}

type SystemdUnit struct {
	Name       string         `json:"name"`
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

type NetworkdSpec struct {
	Enable   *bool                      `json:"enable,omitempty"`
	NetDevs  map[string]NetworkdNetDev  `json:"netdev,omitempty"`
	Networks map[string]NetworkdNetwork `json:"network,omitempty"`
	Source   SourceRef                  `json:"source,omitempty"`
}

type NetworkdSection map[string][]string

type SystemdTimer struct {
	Name    string          `json:"name"`
	Unit    SystemdUnit     `json:"unit"`
	Timer   NetworkdSection `json:"timer,omitempty"`
	Install NetworkdSection `json:"install,omitempty"`
	Enable  *bool           `json:"enable,omitempty"`
	State   string          `json:"state,omitempty"`
	Source  SourceRef       `json:"source,omitempty"`
}

type SystemdResolvedSpec struct {
	Unit    SystemdUnit     `json:"unit"`
	Resolve NetworkdSection `json:"resolve,omitempty"`
	Enable  *bool           `json:"enable,omitempty"`
	State   string          `json:"state,omitempty"`
	Source  SourceRef       `json:"source,omitempty"`
}

type SystemdJournaldSpec struct {
	Unit    SystemdUnit     `json:"unit"`
	Journal NetworkdSection `json:"journal,omitempty"`
	State   string          `json:"state,omitempty"`
	Source  SourceRef       `json:"source,omitempty"`
}

type NetworkdNetDev struct {
	Label          string                     `json:"label"`
	Path           string                     `json:"path"`
	NetDev         NetworkdSection            `json:"netdev,omitempty"`
	WireGuard      NetworkdSection            `json:"wireguard,omitempty"`
	WireGuardPeers map[string]NetworkdSection `json:"wireguard_peer,omitempty"`
	Owner          string                     `json:"owner"`
	Group          string                     `json:"group"`
	Mode           string                     `json:"mode"`
	Ensure         string                     `json:"ensure"`
	Lifecycle      *LifecycleSpec             `json:"lifecycle,omitempty"`
	Summary        ContentSummary             `json:"summary,omitempty"`
	Content        string                     `json:"content,omitempty"`
	Source         SourceRef                  `json:"source,omitempty"`
}

type NetworkdNetwork struct {
	Label     string          `json:"label"`
	Path      string          `json:"path"`
	Match     NetworkdSection `json:"match,omitempty"`
	Network   NetworkdSection `json:"network,omitempty"`
	Owner     string          `json:"owner"`
	Group     string          `json:"group"`
	Mode      string          `json:"mode"`
	Ensure    string          `json:"ensure"`
	Lifecycle *LifecycleSpec  `json:"lifecycle,omitempty"`
	Summary   ContentSummary  `json:"summary,omitempty"`
	Content   string          `json:"content,omitempty"`
	Source    SourceRef       `json:"source,omitempty"`
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

type NftablesSpec struct {
	Enable *bool                       `json:"enable,omitempty"`
	Main   *NftablesFileSpec           `json:"main,omitempty"`
	Files  map[string]NftablesFileSpec `json:"files,omitempty"`
	Source SourceRef                   `json:"source,omitempty"`
}

type NftablesFileSpec struct {
	Label      string         `json:"label"`
	Path       string         `json:"path"`
	Content    string         `json:"content,omitempty"`
	SourcePath string         `json:"source_path,omitempty"`
	Owner      string         `json:"owner"`
	Group      string         `json:"group"`
	Mode       string         `json:"mode"`
	Sensitive  bool           `json:"sensitive,omitempty"`
	Validate   bool           `json:"validate"`
	Activate   bool           `json:"activate"`
	Ensure     string         `json:"ensure"`
	Lifecycle  *LifecycleSpec `json:"lifecycle,omitempty"`
	Summary    ContentSummary `json:"summary,omitempty"`
	Source     SourceRef      `json:"source,omitempty"`
}

type DockerSpec struct {
	Enable   bool                         `json:"enable"`
	Package  DockerPackageSpec            `json:"package"`
	Service  DockerServiceSpec            `json:"service"`
	Daemon   *DockerDaemonSpec            `json:"daemon,omitempty"`
	Users    []string                     `json:"users,omitempty"`
	Composes map[string]DockerComposeSpec `json:"compose,omitempty"`
	Source   SourceRef                    `json:"source,omitempty"`
}

const (
	DockerOfficialRepositoryURL = "https://download.docker.com/linux/debian"
	DockerOfficialGPGURL        = "https://download.docker.com/linux/debian/gpg"
	DockerOfficialGPGSHA256     = "1500c1f56fa9e26b9b8f42452a553675796ade0807cdce11975eb98170b3a570"
)

type DockerPackageSpec struct {
	Source          string    `json:"source"`
	Channel         string    `json:"channel,omitempty"`
	Version         *string   `json:"version,omitempty"`
	RepositoryURL   string    `json:"repository_url,omitempty"`
	GPGURL          string    `json:"gpg_url,omitempty"`
	GPGSHA256       string    `json:"gpg_sha256,omitempty"`
	RemoveConflicts string    `json:"remove_conflicts"`
	SourceRef       SourceRef `json:"source_ref,omitempty"`
}

type DockerServiceSpec struct {
	Enable    bool      `json:"enable"`
	State     string    `json:"state"`
	Name      string    `json:"name"`
	SourceRef SourceRef `json:"source_ref,omitempty"`
}

type DockerDaemonSpec struct {
	Settings map[string]any `json:"settings"`
	Source   SourceRef      `json:"source,omitempty"`
	Summary  ContentSummary `json:"summary,omitempty"`
}

type DockerComposeSpec struct {
	Name          string                           `json:"name"`
	Enable        bool                             `json:"enable"`
	State         string                           `json:"state"`
	Directory     string                           `json:"directory"`
	Project       string                           `json:"project"`
	File          *DockerComposeFileSpec           `json:"file,omitempty"`
	EnvFiles      map[string]DockerComposeFileSpec `json:"env_file,omitempty"`
	Pull          string                           `json:"pull"`
	Recreate      string                           `json:"recreate"`
	RemoveOrphans bool                             `json:"remove_orphans"`
	Service       DockerComposeServiceSpec         `json:"service"`
	After         []string                         `json:"after,omitempty"`
	WantedBy      []string                         `json:"wanted_by,omitempty"`
	Source        SourceRef                        `json:"source,omitempty"`
}

type DockerComposeFileSpec struct {
	Label      string         `json:"label,omitempty"`
	Path       string         `json:"path"`
	Content    string         `json:"content,omitempty"`
	SourcePath string         `json:"source_path,omitempty"`
	Owner      string         `json:"owner"`
	Group      string         `json:"group"`
	Mode       string         `json:"mode"`
	Sensitive  bool           `json:"sensitive,omitempty"`
	Summary    ContentSummary `json:"summary,omitempty"`
	Source     SourceRef      `json:"source,omitempty"`
}

type DockerComposeServiceSpec struct {
	Enable bool   `json:"enable"`
	Name   string `json:"name"`
}

type ContentSummary struct {
	SHA256 string `json:"sha256,omitempty"`
	Bytes  int64  `json:"bytes,omitempty"`
}

type VariableSpec struct {
	Name        string                         `json:"name"`
	Type        string                         `json:"type"`
	TypeExpr    string                         `json:"type_expr,omitempty"`
	TypeSpec    ComponentInputTypeSpec         `json:"type_spec,omitempty"`
	Description string                         `json:"description,omitempty"`
	Default     any                            `json:"default,omitempty"`
	Sensitive   bool                           `json:"sensitive,omitempty"`
	Nullable    bool                           `json:"nullable,omitempty"`
	Ephemeral   bool                           `json:"ephemeral,omitempty"`
	Const       bool                           `json:"const,omitempty"`
	Deprecated  string                         `json:"deprecated,omitempty"`
	Validations []ComponentInputValidationSpec `json:"validations,omitempty"`
	Source      SourceRef                      `json:"source,omitempty"`
}

type ComponentTemplateSpec struct {
	Name         string                                 `json:"name"`
	ArtifactType string                                 `json:"artifact_type,omitempty"`
	Version      string                                 `json:"version,omitempty"`
	Inputs       map[string]ComponentInputSpec          `json:"inputs,omitempty"`
	Scripts      map[string]ComponentScriptSpec         `json:"scripts,omitempty"`
	Sources      map[string]ComponentArtifactSourceSpec `json:"sources,omitempty"`
	Extract      *ComponentArtifactExtractSpec          `json:"extract,omitempty"`
	Build        *ComponentArtifactBuildSpec            `json:"build,omitempty"`
	Install      *ComponentArtifactInstallSpec          `json:"install,omitempty"`
	Source       SourceRef                              `json:"source,omitempty"`
}

type ComponentInputSpec struct {
	Name        string                         `json:"name"`
	Type        string                         `json:"type"`
	TypeExpr    string                         `json:"type_expr,omitempty"`
	TypeSpec    ComponentInputTypeSpec         `json:"type_spec,omitempty"`
	Description string                         `json:"description,omitempty"`
	Default     any                            `json:"default,omitempty"`
	Sensitive   bool                           `json:"sensitive,omitempty"`
	Nullable    bool                           `json:"nullable,omitempty"`
	Deprecated  string                         `json:"deprecated,omitempty"`
	Validations []ComponentInputValidationSpec `json:"validations,omitempty"`
	Source      SourceRef                      `json:"source,omitempty"`
}

type ComponentInputValidationSpec struct {
	ConditionSource SourceRef `json:"condition_source,omitempty"`
	Message         string    `json:"message"`
	MessageSource   SourceRef `json:"message_source,omitempty"`
}

type ComponentScriptSpec struct {
	Name        string     `json:"name"`
	Mode        string     `json:"mode"`
	Body        string     `json:"body,omitempty"`
	Interpreter []string   `json:"interpreter,omitempty"`
	Outputs     []string   `json:"outputs,omitempty"`
	Run         string     `json:"run,omitempty"`
	Content     string     `json:"content,omitempty"`
	Commands    [][]string `json:"commands,omitempty"`
	Sensitive   bool       `json:"sensitive,omitempty"`
	Source      SourceRef  `json:"source,omitempty"`
}

type ComponentInputTypeSpec struct {
	Kind       string                             `json:"kind,omitempty"`
	Element    *ComponentInputTypeSpec            `json:"element,omitempty"`
	Attributes map[string]ComponentObjectAttrSpec `json:"attributes,omitempty"`
	Tuple      []ComponentInputTypeSpec           `json:"tuple,omitempty"`
}

type ComponentObjectAttrSpec struct {
	Type     ComponentInputTypeSpec `json:"type"`
	Optional bool                   `json:"optional,omitempty"`
	Default  any                    `json:"default,omitempty"`
}

type ComponentInstanceSpec struct {
	Name           string                         `json:"name"`
	Template       string                         `json:"template"`
	InputValues    map[string]any                 `json:"input_values,omitempty"`
	ArtifactType   string                         `json:"artifact_type,omitempty"`
	Version        string                         `json:"version,omitempty"`
	Scripts        map[string]ComponentScriptSpec `json:"scripts,omitempty"`
	SelectedSource *ComponentArtifactSourceSpec   `json:"selected_source,omitempty"`
	Extract        *ComponentArtifactExtractSpec  `json:"extract,omitempty"`
	Build          *ComponentArtifactBuildSpec    `json:"build,omitempty"`
	Install        *ComponentArtifactInstallSpec  `json:"install,omitempty"`
	APT            APTSpec                        `json:"apt"`
	Packages       PackageSpec                    `json:"packages"`
	Files          FileSpec                       `json:"files"`
	Secrets        SecretSpec                     `json:"secrets"`
	Directories    DirectorySpec                  `json:"directories"`
	Groups         GroupSpec                      `json:"groups"`
	Users          UserSpec                       `json:"users"`
	Systemd        SystemdSpec                    `json:"systemd"`
	Services       ServiceSpec                    `json:"services"`
	Source         SourceRef                      `json:"source,omitempty"`
}

type ComponentArtifactSourceSpec struct {
	Architecture string    `json:"architecture,omitempty"`
	URL          string    `json:"url"`
	SHA256       string    `json:"sha256"`
	Source       SourceRef `json:"source,omitempty"`
}

type ComponentArtifactExtractSpec struct {
	Format          string    `json:"format"`
	StripComponents int       `json:"strip_components,omitempty"`
	Include         string    `json:"include,omitempty"`
	Source          SourceRef `json:"source,omitempty"`
}

type ComponentArtifactBuildSpec struct {
	Commands   [][]string `json:"commands,omitempty"`
	Packages   []string   `json:"packages,omitempty"`
	WorkingDir string     `json:"working_dir,omitempty"`
	Output     string     `json:"output"`
	SourceName string     `json:"source_name,omitempty"`
	Source     SourceRef  `json:"source,omitempty"`
}

type ComponentArtifactInstallSpec struct {
	Path   string    `json:"path"`
	Owner  string    `json:"owner"`
	Group  string    `json:"group"`
	Mode   string    `json:"mode"`
	Source SourceRef `json:"source,omitempty"`
}
