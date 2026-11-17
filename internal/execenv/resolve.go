package execenv

// Which environment a step runs in (EV2), and the one tightening the daemon
// does on its own (EV6).
//
// Two properties hold this file together:
//
//   - **No code fallback.** No binding anywhere is a REFUSAL with the sentence
//     that names the command to run. The alternative — a built-in default — is
//     the state where a workspace silently runs with whatever the last
//     refactor thought was safe. Fail-closed is the same posture as a `bash`
//     with no verdict source (F6).
//   - **No silent downgrade, with one named exception.** EV6 is the only
//     change the daemon makes to what a person bound, it goes strictly toward
//     restrictive, and it is RECORDED (`Downgraded` + `Reason`) so the step
//     record, the panel and the event all say the environment was changed and
//     why. Anything else that cannot run is refused.
//
// Resolution is per step and read live (EV2): tightening an environment takes
// effect on the next command, which is the whole point of a record rather than
// a build-time constant.

// BoundEnv is one environment as bound to a scope. It carries the fields the
// decision reads; the store hands these over, and the resolver never opens a
// database.
type BoundEnv struct {
	ID      string
	Slug    string
	Backend string
	Legacy  bool
	Origin  string
	Spec    Spec
}

// ResolveInput is the three lookups plus what the caller knows about the loop.
// A nil pointer means "no binding at that scope" — not "unknown", which is why
// the store's BindingFor returns a source string alongside the row.
type ResolveInput struct {
	ComponentBinding *BoundEnv
	RootBinding      *BoundEnv
	WorkspaceBinding *BoundEnv

	// Attended is loop.attended, three-state: true, false, or nil for
	// "never declared" (F10). EV6 reads nil the way execute.go does — as
	// unattended — because "nobody ever said" is not "somebody said someone is
	// watching".
	Attended *bool

	// ScrubbedSystem is the system `host-scrubbed` row. Nil means it has been
	// removed, and then a downgrade is a REFUSAL, never an inherit (EV6).
	ScrubbedSystem *BoundEnv
}

// Resolution is what the step runs in, and how we got there.
type Resolution struct {
	Env    BoundEnv `json:"env"`
	Source string   `json:"source"` // component | root | workspace
	// Downgraded is EV6's mark: the bound record was the legacy one and the
	// daemon tightened it. The bound record is never rewritten; the step says
	// what actually ran.
	Downgraded bool   `json:"downgraded"`
	Reason     string `json:"reason,omitempty"`
}

// The resolution sources. `downgrade` is not a source — the source is always
// where the binding was found, and `Downgraded` says what happened to it.
const (
	SourceComponent = "component"
	SourceRoot      = "root"
	SourceWorkspace = "workspace"
)

// Resolve applies EV2's order and EV6's rule. Pure over the three lookups: a
// table with fifteen rows is the whole test surface, no store, no daemon.
func Resolve(in ResolveInput) (Resolution, *Refusal) {
	picked, source := pickBinding(in)
	if picked == nil {
		return Resolution{}, NewRefusal(RefusalEnvironmentUnbound,
			"no execution environment is bound for this workspace",
			"there is no binding on the component, none on its root component, and none on the workspace, "+
				"and this slice has no built-in default to fall back to",
			"run `aeon env bind --environment host-scrubbed` (or open Settings → Environments and bind one)")
	}

	env := *picked
	downgraded := false
	reason := ""

	if env.Legacy || env.Backend == BackendHostInherit {
		if !attendedOrUnknownIsUnattended(in.Attended) {
			// Attended, on a store that upgraded: today's behaviour exactly,
			// until a person binds something stronger (EV20's continuity).
			return Resolution{Env: env, Source: source}, nil
		}
		if in.ScrubbedSystem == nil {
			return Resolution{}, NewRefusal(RefusalEnvironmentUnavailable,
				"the bound environment "+env.Slug+" is the inherited one and this loop is not attended",
				"an unattended loop must never inherit the daemon's environment, and the system "+
					"host-scrubbed environment is not available to run in instead",
				"restore or create a host-scrubbed environment and bind it (`aeon env bind --environment host-scrubbed`)")
		}
		env = *in.ScrubbedSystem
		downgraded = true
		reason = "loop is unattended; the inherited environment is refused"
	}

	return Resolution{Env: env, Source: source, Downgraded: downgraded, Reason: reason}, nil
}

// attendedOrUnknownIsUnattended is EV6 in one line, and it is a function
// because the four cells (attended true/false/nil × legacy/non-legacy) are the
// test, and each cell must read the same source of truth.
func attendedOrUnknownIsUnattended(attended *bool) bool {
	return attended == nil || !*attended
}

func pickBinding(in ResolveInput) (*BoundEnv, string) {
	if in.ComponentBinding != nil {
		return in.ComponentBinding, SourceComponent
	}
	if in.RootBinding != nil {
		return in.RootBinding, SourceRoot
	}
	if in.WorkspaceBinding != nil {
		return in.WorkspaceBinding, SourceWorkspace
	}
	return nil, ""
}

// EffectiveIsLegacy answers the doctor's and the panel's question (EV6): is
// the workspace default still the inherited row? It is a question about the
// workspace binding alone, so it does not need a component.
func EffectiveIsLegacy(workspace *BoundEnv) bool {
	return workspace != nil && (workspace.Legacy || workspace.Backend == BackendHostInherit)
}
