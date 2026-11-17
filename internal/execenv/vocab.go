package execenv

// Everything a client would otherwise re-type lives here as DATA (EV3, EV4,
// EV8). `environment.vocabulary` serves this same struct to the app, the CLI
// and the tool descriptions, so "what may I ask for" has one answer in the
// product. A vocabulary a client types again is a vocabulary that drifts.

// The backend names. These four strings are the whole vocabulary; a fifth is
// a migration, a capability row and a driver, never a string into a flag.
const (
	BackendHostInherit  = "host-inherit"
	BackendHostScrubbed = "host-scrubbed"
	BackendContainer    = "container"
	BackendVM           = "vm"
	EnvSystemScrubbed   = "env-host-scrubbed"
	EnvLegacyRow        = "env-host-inherit"
)

// Capability names, served by Capabilities and enforced by Validate. The set
// is closed: a spec may only ask for what its backend can enforce, and a
// refusal says which backend cannot do the thing.
const (
	CapEnvAllowlist     = "env-allowlist"
	CapSecretsRedacted  = "secrets-redacted"
	CapFilesystemIsolat = "filesystem-isolation"
	CapEgressNone       = "egress-none"
	CapResourceLimits   = "resource-limits"
	CapWallLimit        = "wall-limit"
	CapServices         = "services"
)

// Backend is one row of EV3's table.
type Backend struct {
	Name         string
	Strength     int
	Built        bool
	Capabilities []string // the capability names this backend can enforce
	Description  string
}

// Backends returns the four backends in strength order, weakest first. The
// order is load-bearing: EV6's downgrade and EV16's Restricts both compare
// strength, so it is data here and not an accident of a map iteration.
func Backends() []Backend {
	return []Backend{
		{
			Name: BackendHostInherit, Strength: 0, Built: true,
			Capabilities: []string{CapWallLimit},
			Description: "the legacy backend: the child inherits the daemon's whole environment. " +
				"Kept for continuity on stores that predate S59; never used by an unattended loop (EV6).",
		},
		{
			Name: BackendHostScrubbed, Strength: 1, Built: true,
			Capabilities: []string{CapWallLimit, CapEnvAllowlist, CapSecretsRedacted},
			Description: "the default: the child gets an allowlisted, built environment and injected " +
				"secret references, with output redacted. Not a sandbox — it still runs as the daemon's user.",
		},
		{
			Name: BackendContainer, Strength: 2, Built: true,
			Capabilities: []string{CapWallLimit, CapEnvAllowlist, CapSecretsRedacted,
				CapFilesystemIsolat, CapEgressNone, CapResourceLimits, CapServices},
			Description: "one labelled container per loop, one bind mount (the step's working root), " +
				"cap-drop ALL, no-new-privileges, optional --network none, per-loop services.",
		},
		{
			Name: BackendVM, Strength: 3, Built: false,
			Capabilities: []string{CapWallLimit, CapEnvAllowlist, CapSecretsRedacted,
				CapFilesystemIsolat, CapEgressNone, CapResourceLimits, CapServices},
			Description: "specified, not built: the slot a Parallels/VM driver registers into. " +
				"Creatable only once a driver registers with this daemon.",
		},
	}
}

// Capabilities answers EV3's question for one backend: what it can enforce.
// Unknown backend ⇒ nil, and Validate refuses the name rather than defaulting.
func Capabilities(backend string) []string {
	for _, b := range Backends() {
		if b.Name == backend {
			return b.Capabilities
		}
	}
	return nil
}

// HasCapability reports whether a backend can enforce one capability.
func HasCapability(backend, cap string) bool {
	for _, c := range Capabilities(backend) {
		if c == cap {
			return true
		}
	}
	return false
}

// BackendStrength is EV3's strength order. An unknown backend scores -1, which
// is below everything, so an unrecognised name can never compare as "stronger"
// and let a widening through.
func BackendStrength(backend string) int {
	for _, b := range Backends() {
		if b.Name == backend {
			return b.Strength
		}
	}
	return -1
}

// EgressModes is the closed set. `allow` is deliberately absent from this
// list and present in the spec type: it is a shape that is declared, refused
// with a reason, and staged — not a field a caller cannot even name.
func EgressModes() []string { return []string{"unenforced", "none", "unrestricted"} }

// egressRank orders the modes for Restricts: a LOWER rank is looser. Both
// `unenforced` (the host's honest "we do not try") and `unrestricted` (the
// container's honest "everything is reachable") sit below `none`, so moving
// either way to `none` is a tightening and moving out of it is a widening.
func egressRank(mode string) int {
	switch mode {
	case "none":
		return 2
	case "unrestricted":
		return 1
	case "unenforced", "":
		return 0
	default:
		return -1 // unknown: below everything, so it never reads as tighter
	}
}

// SourceKinds is EV7's closed set of places a secret reference may point at.
// `store` is built (T0's approval A1 was granted and read on 2026-11-03): it
// resolves through internal/secrets' credential file, never a database column.
func SourceKinds() []string { return []string{"daemon-env", "key-file", "store"} }

// PullPolicies is the image pull vocabulary.
func PullPolicies() []string { return []string{PullNever, PullMissing} }

// HomeModes is the HOME vocabulary.
func HomeModes() []string { return []string{HomeReal, HomeScratch} }

// DefaultPassthrough is the allowlist the seed's system environment carries
// (EV19). It is written out in the seeded row's spec rather than rebuilt at
// spawn, so `environment.show` reads the same list the child got.
//
// Two omissions are decisions, not oversights: `CI` is not set by default
// because it changes what tools do (D4), and nothing that looks like a
// credential is here because Deny would refuse it anyway.
func DefaultPassthrough() []string {
	return []string{"PATH", "HOME", "USER", "LOGNAME", "SHELL", "LANG", "LC_*", "TZ", "TMPDIR"}
}

// DenyList is the list that wins even when an operator named the variable
// explicitly (EV4, the PL4 posture). It is four shapes because a name can be
// refused four ways, and a rule that only checks exact matches is defeated by
// a prefix.
//
// Being a superset of config's credential marks is not a courtesy: the same
// word has to mean "credential" in both places or one of them leaks while the
// other one is confident. A test in this package asserts the superset against
// config's own list, so the two cannot drift apart quietly.
type DenyList struct {
	// Words are substrings, matched case-insensitively, that make a NAME a
	// credential by shape: `MY_APP_TOKEN`, `db_password`, `GH_TOKEN`.
	Words []string
	// Prefixes are namespace prefixes: nothing in a vendor's namespace belongs
	// in a child that a model is driving.
	Prefixes []string
	// Exact are names that are a credential or an agent handle whatever their
	// spelling: an ssh agent socket is not a locale variable.
	Exact []string
	// Loader are the dynamic-loader and shell-injection variables. They are
	// separate from Prefixes because their denial is the reason the list
	// exists at all: `LD_PRELOAD` in a child is code execution as the
	// daemon's user, which is exactly what EV1 says host-scrubbed does NOT
	// protect against — so it is refused on every backend, including legacy's
	// own builder path, and never negotiable by a spec.
	Loader []string
	// Exempt are prefixes that escape Prefixes and nothing else. EV14 makes
	// the daemon itself build `AEON_SERVICE_<NAME>_HOST` / `_PORT` for every
	// service container, and `AEON_` is a denied namespace — so without an
	// exemption the daemon would refuse to inject the names it derives. Words
	// still apply inside an exempt name: a service called `token` still gets
	// refused, because the exemption is about the daemon's own namespace, not
	// about credentials.
	Exempt []string
	// SetOnly are names a spec may PASS THROUGH but not SET: copying the
	// daemon's PATH is the allowlist doing its job; deciding the child's PATH
	// by literal is a different act, and one that turns any relative command
	// into whatever binary the spec pointed at.
	SetOnly []string
}

// DenyList returns the closed deny set.
func Deny() DenyList {
	return DenyList{
		Words: []string{
			"KEY", "TOKEN", "SECRET", "PASSWORD", "PASSWD", "CREDENTIAL", "AUTH", "COOKIE",
			"APIKEY", "API_KEY", "ACCESS_KEY", "PRIVATE",
		},
		Prefixes: []string{
			"AEON_", "ANTHROPIC_", "OPENAI_", "GITHUB_", "GH_", "AWS_", "AZURE_", "GOOGLE_", "GCP_",
			"GITLAB_", "BITBUCKET_", "SLACK_", "STRIPE_", "NPM_", "DOCKER_",
		},
		Exact: []string{
			"SSH_AUTH_SOCK", "GPG_AGENT_INFO", "GPG_AGENT_ADDRESS", "DOCKER_HOST", "KUBECONFIG",
			"DBUS_SESSION_BUS_ADDRESS", "DISPLAY", "WAYLAND_DISPLAY",
		},
		Loader: []string{
			"LD_PRELOAD", "LD_LIBRARY_PATH", "LD_AUDIT", "DYLD_INSERT_LIBRARIES", "DYLD_LIBRARY_PATH",
			"BASH_ENV", "ENV", "SHELLOPTS", "BASHOPTS", "PROMPT_COMMAND", "PS4", "IFS", "GLOBIGNORE",
			"PYTHONSTARTUP", "PYTHONPATH", "NODE_OPTIONS", "NODE_REPL_EXTERNAL_MODULE", "PERL5OPT",
			"RUBYOPT", "RUBYLIB", "RUST_MIN_STACK", "GIT_CONFIG", "GIT_CONFIG_GLOBAL", "GIT_CONFIG_SYSTEM",
			"GIT_SSH", "GIT_SSH_COMMAND", "GIT_EXEC_PATH", "GIT_INDEX_FILE", "GIT_DIR", "GIT_WORK_TREE",
			"GIT_OBJECT_DIRECTORY", "GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_SEQUENCE_EDITOR", "GIT_EDITOR",
			"EDITOR", "PAGER",
		},
		Exempt:  []string{"AEON_SERVICE_"},
		SetOnly: []string{"PATH", "HOME", "TMPDIR", "LD_PRELOAD"},
	}
}

// ProxyWarnPatterns is EV4's honest middle: a name that MIGHT carry a
// credential in its value is warned about, not refused, because a corporate
// proxy without `HTTPS_PROXY` is no proxy at all. The warning's sentence
// points at secret references, which is where a credentialed URL belongs.
func ProxyWarnPatterns() []string { return []string{"PROXY"} }

// MaxSetEntries and MaxSetValueBytes are EV4's bounds on the literal map. A
// spec is data a person reads; an unbounded map turns `environment.show` into
// a wall and turns a diff into noise.
const (
	MaxSetEntries     = 64
	MaxSetValueBytes  = 1024
	MaxVarNameLen     = 128
	MaxWallSeconds    = 600 // tools.BashTimeout — a spec may only shorten the ceiling (F4)
	MinMemoryMiB      = 64
	MinCPUs, MaxCPUs  = 0.1, 64.0
	MaxServices       = 8
	MaxSecretRefs     = 32
	MaxPassthroughLen = 64
)

// Honesty is EV1's sentence, and there is exactly one copy of it. The bash
// tool description, environment.vocabulary, the Settings panel and
// `aeon env show` print it verbatim — the BashHonesty pattern, one formatter —
// because a limit nobody was told about is one they will cross without meaning
// to.
const Honesty = "host-scrubbed removes ambient secrets from what a command sees; it is not a " +
	"sandbox. The command still runs as the daemon's user, can read every file that user can " +
	"read, and can reach the network. Filesystem and network isolation come from the container " +
	"backend."

// RedactionLimits is EV8's honest second line, in the same voice and for the
// same reason. Redaction is what catches the secret that was printed; it is
// not what keeps the secret out of reach.
const RedactionLimits = "Redaction replaces an injected secret's exact bytes and their base64 " +
	"(standard and URL-safe, padded and unpadded), hex and percent forms in a command's output, " +
	"across chunk boundaries. It does not catch a secret the command transformed (reversed, " +
	"split by a character, XORed, ROT13), one written to a file and read back with the read " +
	"tool, or one a model produced from other knowledge. The first line of defence is that only " +
	"the commands that need a variable get it. TestRedactLimitsAreTrue asserts this sentence " +
	"and nothing wider."

// VocabularySet is everything a client would otherwise re-type, in one call.
type VocabularySet struct {
	Backends         []Backend `json:"backends"`
	DefaultAllowed   []string  `json:"defaultPassthrough"`
	Deny             DenyList  `json:"deny"`
	ProxyWarn        []string  `json:"proxyWarnPatterns"`
	EgressModes      []string  `json:"egressModes"`
	SourceKinds      []string  `json:"sourceKinds"`
	PullPolicies     []string  `json:"pullPolicies"`
	HomeModes        []string  `json:"homeModes"`
	CapabilityNames  []string  `json:"capabilities"`
	MaxSetEntries    int       `json:"maxSetEntries"`
	MaxSetValueBytes int       `json:"maxSetValueBytes"`
	MaxWallSeconds   int       `json:"maxWallSeconds"`
	Honesty          string       `json:"honesty"`
	RedactionLimits  string       `json:"redactionLimits"`
	KnownLimits      []KnownLimit `json:"knownLimits"`
}

// KnownLimit is a limitation the platform knows about, said by the daemon, with
// whether it is true RIGHT NOW. The Settings panel prints these verbatim and
// holds no status of its own (EV15): a panel that hardcoded "parallel units
// share one working tree" would go on saying it on the day the worktrees land,
// which is how a fixed defect becomes folklore. `Active` is the truth the daemon
// asserts; `Sentence` is the words it asserts it in.
type KnownLimit struct {
	Key      string `json:"key"`
	Sentence string `json:"sentence"`
	Active   bool   `json:"active"`
}

// SharedWorktreeSentence is EV15's warning, kept here for the same reason the
// honesty line is: one copy, spoken by the daemon, printed by every surface.
const SharedWorktreeSentence = "parallel units of one loop still share a single working tree: each " +
	"unit gets its own worktree created and ledgered, but the model step runs with the shared " +
	"component root as its workspace, so two units editing the same file overwrite each other. " +
	"This is not an execution-environment defect; the fix is S32's follow-up."

// SharedWorktreeLimit is whether that sentence is true today. T0 proved it
// 2026-11-03 with a throwaway over `parallelGroupBlueprint`: every unit's
// worktree is created and ledgered while `runOneUnit` hands the model step the
// shared root (F8/F9 in the task's inventory). It is a named constant rather
// than a live probe because there is nothing to probe at read time — the fact
// lives in which path `runOneUnit` passes. The day that changes, this flips and
// the panel stops saying it. Nobody may type the status into a UI.
const SharedWorktreeLimit = true

func KnownLimits() []KnownLimit {
	return []KnownLimit{{
		Key: "s32-shared-working-tree", Sentence: SharedWorktreeSentence, Active: SharedWorktreeLimit,
	}}
}

// Vocabulary serves the whole vocabulary. It is a function, not a variable, so
// a driver registering a `vm` backend at start (T9) is reflected without a
// second source of truth.
func Vocabulary() VocabularySet {
	caps := []string{CapEnvAllowlist, CapSecretsRedacted, CapFilesystemIsolat,
		CapEgressNone, CapResourceLimits, CapWallLimit, CapServices}
	return VocabularySet{
		Backends:        Backends(),
		DefaultAllowed:  DefaultPassthrough(),
		Deny:            Deny(),
		ProxyWarn:       ProxyWarnPatterns(),
		EgressModes:     EgressModes(),
		SourceKinds:     SourceKinds(),
		PullPolicies:    PullPolicies(),
		HomeModes:       HomeModes(),
		CapabilityNames: caps,
		MaxSetEntries:   MaxSetEntries, MaxSetValueBytes: MaxSetValueBytes,
		MaxWallSeconds: MaxWallSeconds,
		Honesty:        Honesty, RedactionLimits: RedactionLimits,
		KnownLimits:    KnownLimits(),
	}
}
