package config

// settings_decl_intake.go — S61 T7 declares the two daemon-wide values the
// goal-intake plane owns: whether the daemon may create a remote repository
// at all (GI12's default-deny key), and where a person's project trees
// live (GI9's target guard reads the workspace root from here, never from
// a flag or an environment accident).
//
// Why settings and not constants: both values decide whether a PERSON's
// act lands where they meant. `intake.allowRemoteCreate` is the outward
// half of a commit — an outward act that is off by default and readable
// in the panel is P31's honesty applied to scaffolding; `intake.workspaceRoot`
// is the one directory the target guard compares every plan target
// against, and a root a person can set from the panel is a root that
// cannot silently move when a shell export differs between machines.
//
// The shapes follow S57's durations, not S62's int counts: a bool and a
// path are what a person reads and types; a missing workspace root is a
// refusal WITH its sentence at the door that needs it, not a fallback that
// invents a directory.
func init() {
	mustDeclare(Setting{
		Key:     "intake.allowRemoteCreate",
		Area:    "intake",
		Type:    TypeBool,
		Default: "false",
		Description: "Whether an intake commit may create a remote repository (forge.createRepo, S61 GI12). " +
			"Off by default: an outward act needs the person to turn it on, and an unwired or unreadable read is that same default. " +
			"Turning it on permits the attempt — the plan's publish choice and the commit's confirmation still decide per commit, " +
			"and a create with a missing credential refuses naming what is missing.",
	})
	mustDeclare(Setting{
		Key:     "intake.workspaceRoot",
		Area:    "intake",
		Type:    TypeString,
		Default: "",
		MaxLen:  4096,
		Description: "The one directory intake scaffold targets live under (S61 GI9): the target guard refuses a path outside it, " +
			"a path that is an existing component root, or a non-empty directory. Empty (the default) means no default target exists: " +
			"a plan without an explicit targetPath refuses with this sentence instead of inventing a directory to write a person's project into.",
	})
}
