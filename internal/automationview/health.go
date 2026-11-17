package automationview

// health.go — the advisory verdict, composed from facts the daemon already
// read (T3): blueprint present/valid via S48, secret env set-or-unset,
// workspace and scope existing. It is a pure function of its inputs — no
// clock, no environment, no files — and it never gates a fire (AN8: health
// is advice; the engine's answer is the fire log).

// BlueprintState values the composer accepts, from the S48 read.
const (
	BlueprintOK           = "ok"
	BlueprintMissing      = "missing"
	BlueprintInvalid      = "invalid"
	BlueprintUnverifiable = "unverifiable" // the S48 seam is not wired — health says so, never guesses
)

// HealthInputs are already-read facts. Naming them all booleans/enums
// keeps the composer total: every absent fact arrived as a named state, so
// "unknown" is a value, never a missing field.
type HealthInputs struct {
	BlueprintState    string // one of the four constants
	BlueprintErrors   int    // report counts ride along for the UI's detail row
	BlueprintWarnings int
	SecretEnvSet      bool // webhook family; ignored otherwise
	SecretEnvName     string
	WorkspaceExists   bool
	ScopeExists       bool // the component/root the scope names still exists
	Enabled           bool
	Paused            bool
	Archived          bool
	IsWebhook         bool
}

// Check is one line of the health panel.
type Check struct {
	Name  string
	State string // "ok" | "missing" | "invalid" | "unset" | "absent" | "off" | "unverifiable"
	Note  string
}

// Health is the composed verdict.
type Health struct {
	Checks  []Check
	Reasons []string // human sentences, in Checks order, for every non-ok line
	CanFire bool     // every check ok AND enabled AND not archived AND not paused
}

// ComposeHealth folds the facts into the verdict. The order is stable:
// blueprint, secret (webhooks), workspace, scope, then the switches — the
// UI renders rows in this order without sorting.
func ComposeHealth(in HealthInputs) Health {
	h := Health{}
	add := func(name, state, okNote, badNote string, good bool) {
		c := Check{Name: name, State: "ok", Note: okNote}
		if !good {
			c.State, c.Note = state, badNote
			if badNote != "" {
				h.Reasons = append(h.Reasons, badNote)
			}
		}
		h.Checks = append(h.Checks, c)
	}
	switch in.BlueprintState {
	case BlueprintOK:
		note := "blueprint present and valid"
		if in.BlueprintWarnings > 0 {
			note += " (with warnings)"
		}
		add("blueprint", "ok", note, "", true)
	case BlueprintMissing:
		add("blueprint", "missing", "", "the blueprint this automation fires does not exist — a fire would start nothing", false)
	case BlueprintInvalid:
		add("blueprint", "invalid", "", "the blueprint fails validation — a fire would start nothing", false)
	default:
		add("blueprint", BlueprintUnverifiable, "", "the blueprint read is not wired — health cannot say whether it is there", false)
	}
	if in.BlueprintState == BlueprintOK && in.BlueprintWarnings > 0 {
		// A warning never blocks CanFire; it prints as its own ok-row note.
		h.Checks[len(h.Checks)-1].Note += " (advisory)"
	}
	if in.IsWebhook {
		if in.SecretEnvName == "" {
			add("secret", "unset", "", "no secret env is named — the webhook can never verify a signature", false)
		} else if in.SecretEnvSet {
			add("secret", "ok", "env "+in.SecretEnvName+" is set (the value stays unread)", "", true)
		} else {
			add("secret", "unset", "", "env "+in.SecretEnvName+" is not set in the daemon — every POST will be refused", false)
		}
	}
	add("workspace", "absent", "the workspace directory exists", "the workspace directory does not exist", in.WorkspaceExists)
	add("scope", "absent", "the scoped component still exists", "the component or root this automation is scoped to no longer exists", in.ScopeExists)
	add("switches", "ok", "", "", in.Enabled && !in.Archived && !in.Paused)
	if in.Archived {
		h.Reasons = append(h.Reasons, "archived — the engines do not see it (unarchive first, then enable)")
	} else if !in.Enabled {
		h.Reasons = append(h.Reasons, "disabled — enable it when a person is present")
	}
	if in.Paused {
		h.Reasons = append(h.Reasons, "paused — the clock skips it until resumed")
	}
	allOK := true
	for _, c := range h.Checks {
		if c.State != "ok" {
			allOK = false
		}
	}
	h.CanFire = allOK && in.Enabled && !in.Archived && !in.Paused
	return h
}
