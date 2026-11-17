package config

// settings_decl_scheduler.go — S27 T8 declares the four daemon-wide numbers
// the continuous scheduler spends (CN21). One file per area (CS3), so a
// declaration is never a merge conflict with another slice's.
//
// Why settings and not constants: all four are numbers whose right value
// depends on the machine and the day — a laptop with two loops and a CI box
// with twenty want different slot counts — and an operator who cannot set
// them unsets them by killing loops by hand, the same act with no ledger
// behind it. Declaring them here makes each value readable in the
// Continuous panel (the daemon answers it, CN22), writable through
// `settings.set` on the human door, and auditable in `setting_event`.
//
// What is deliberately NOT here (CN21, and TestNoAuthorityKeyInTheRegistry
// fails anyone who adds one): the scheduler's AUTHORITY — the master switch
// and the per-repository rows — stays in the scheduler's own tables, written
// only through the eight human-door controls (S27 CN2) and audited there.
// A `scheduler.enabled` setting would put standing authority behind a
// generic door that stamps no reasoning, and that is the exact shape AU1
// exists to refuse. These four keys are numbers the plan spends; none of
// them turns the scheduler on.
//
// None is Sensitive; none carries a credential-shaped name (CS5); each
// honours its environment override (CS4, the layer order is environment →
// row → default). The read sites (internal/daemon/continuous.go) take them
// through config.Reader FRESH each tick (CS7) — a change lands on the next
// tick and touches no running loop.
func init() {
	// Shared bound pointers: named once, compared by value. The bounds are
	// the spec's (S27 T8's table) and the shapes are D2's law — a count,
	// never a free quantity: slots cap what the machine runs at once, the
	// tick caps how often it looks, the backstop caps one repository's hour,
	// aging caps how long fairness waits.
	var (
		minOne    = 1
		minTick   = 5
		maxSlots  = 64
		maxTick   = 300
		maxStarts = 60
		maxAging  = 720
	)
	mustDeclare(Setting{
		Key:     "scheduler.maxAgentSlots",
		Area:    "scheduler",
		Type:    TypeInt,
		Default: "2",
		Min:     &minOne,
		Max:     &maxSlots,
		Description: "Agent slots the daemon may use at once across all loops; each running " +
			"loop takes its repository's parallel-agent setting. A count, not a memory limit. " +
			"(S27 CN9 — the scheduler never starts past this ceiling.)",
	})
	mustDeclare(Setting{
		Key:     "scheduler.tickSeconds",
		Area:    "scheduler",
		Type:    TypeInt,
		Default: "15",
		Min:     &minTick,
		Max:     &maxTick,
		Description: "How often the scheduler looks for work. Finished loops wake it sooner — " +
			"this is the floor of patience, not the only moment it wakes. A change takes " +
			"effect on the next tick. (S27 CN18's cadence.)",
	})
	mustDeclare(Setting{
		Key:     "scheduler.maxStartsPerRootPerHour",
		Area:    "scheduler",
		Type:    TypeInt,
		Default: "6",
		Min:     &minOne,
		Max:     &maxStarts,
		Description: "A backstop: the most loops the scheduler will start in one repository in " +
			"an hour. A runaway plan shows up here as deferrals, not as a machine full of " +
			"loops. (S27 CN12.)",
	})
	mustDeclare(Setting{
		Key:     "scheduler.agingHours",
		Area:    "scheduler",
		Type:    TypeInt,
		Default: "24",
		Min:     &minOne,
		Max:     &maxAging,
		Description: "After this long deferred for capacity, a unit moves to the front of the " +
			"line — the young do not starve the old forever. (S27 CN11.)",
	})
}
