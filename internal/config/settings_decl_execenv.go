package config

// settings_decl_execenv.go — S59 T6 declares the one daemon-wide number the
// execution-environment plane owns: how long an idle provision's container
// stays before the reconcile sweep takes it (EV12's lifecycle, D7's 24 hours).
//
// Why a setting and not a constant: the sweep is the difference between "a
// loop's container waits for its next step" and "a machine accumulates
// containers for loops that finished last week". The right number depends on
// how a person uses the machine — a CI box that re-runs one loop every ten
// minutes wants a long TTL; a laptop with 8 GiB wants a short one — and an
// operator who cannot set it unsets it by killing containers by hand, which is
// the same act with no ledger behind it. Declaring it here is what makes the
// value readable in the panel, writable through `settings.set` (the human
// door, CS9), and auditable in `setting_event` instead of living in a shell
// export nobody remembers.
//
// The shape follows S57's two gates durations (a duration's canonical text
// form, read at the site that uses it), not S62's int counts: `24h` is what a
// person types, and a value that will not parse falls back to the default AT
// THE READ SITE with the reason spoken — never a daemon that will not start
// because somebody typed `a day`.
func init() {
	mustDeclare(Setting{
		Key:     "execenv.provisionIdleTTL",
		Area:    "execenv",
		Type:    TypeString,
		Default: "24h",
		Description: "How long a provision's container survives with no step using it before the reconcile sweep removes it (S59 EV12). " +
			"A duration like 24h or 90m. Longer keeps a loop's warm container between steps; shorter frees the machine sooner. " +
			"A value that will not parse falls back to 24h and says so, and the sweep never removes a container a running step is using.",
	})
}
