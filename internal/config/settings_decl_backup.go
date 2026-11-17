package config

// settings_decl_backup.go — S62 T10's adoption of the ten backup.* keys
// (BK11) onto the config store. The keys, the defaults and the
// pre-migration exception do not move with the adoption — this file only
// gives the keys a declaration, so the value a run reads comes from
// environment → database → default (CS4) instead of environment → default,
// and the panel and the CLI can edit the database layer.
//
// Two type choices, both deliberate:
//
//   - the three durations are TypeString, exactly as gates.watchWindow and
//     gates.watchInterval are (S57 T7's precedent): a duration's canonical
//     text form ("24h", "10m") is the value, parsed at the read site. A
//     TypeInt declaration would have changed the meaning of the established
//     AEON_BACKUP_* environment variables, which existing operators export
//     as durations — the adoption must not break them.
//   - the four retention counts are TypeInt with Min 1: a keep of zero
//     deletes everything it keeps, so the declaration itself refuses it at
//     set time (TestInvalidSettingRefusedAtSetTime).
//
// What the declaration CANNOT carry — and where it is still enforced:
// the interval floor (fifteen minutes) and the absolute-path rule for
// backup.dir are semantic, not type-level, so they stay at the resolver
// (backup.ResolveSettings), which refuses with a sentence naming the key.
// A value that passes the declaration but fails the resolver is refused
// at run time exactly as a bad environment value always was.
//
// None of the ten is Sensitive (CS5): none is credential-shaped, and the
// setting table is a settings store, not a secret store.
//
// init() ordering: settings.go's init sets defaultRegistry to empty, then
// this file's init (settings_decl_backup.go sorts after settings.go —
// "." < "_") mustDeclare's the ten keys into it, beside the agents and
// gates declarations the same way.
func init() {
	// minOne is the shared Min pointer for the four retention counts.
	var minOne = 1
	mustDeclare(Setting{
		Key:     "backup.enabled",
		Area:    "backup",
		Type:    TypeBool,
		Default: "true",
		Description: "Run backups at all. The default (true) keeps the safety net on; " +
			"turning it off stops the scheduled runs — manual runs still work.",
	})
	mustDeclare(Setting{
		Key:     "backup.dir",
		Area:    "backup",
		Type:    TypeString,
		Default: "",
		Description: "Where backups are stored. Empty means <state dir>/backups; a set " +
			"value must be an absolute path. The pre-migration hook cannot read this " +
			"setting — the table is inside the very database it protects — so a " +
			"pre-migration backup always lands in the default dir.",
	})
	mustDeclare(Setting{
		Key:     "backup.interval",
		Area:    "backup",
		Type:    TypeString,
		Default: "24h",
		Description: "How often the scheduled backup runs. A duration such as 1h or 24h; " +
			"fifteen minutes is the floor — shorter than the store can safely be " +
			"copied in, so a shorter interval is refused, not clamped.",
	})
	mustDeclare(Setting{
		Key:     "backup.timeout",
		Area:    "backup",
		Type:    TypeString,
		Default: "10m",
		Description: "How long one backup run may take before it is called a failure. " +
			"A duration such as 10m.",
	})
	mustDeclare(Setting{
		Key:         "backup.keep",
		Area:        "backup",
		Type:        TypeInt,
		Default:     "5",
		Min:         &minOne,
		Description: "How many recent backups to keep; older ones are pruned unless a daily or weekly keep holds them.",
	})
	mustDeclare(Setting{
		Key:         "backup.keepDaily",
		Area:        "backup",
		Type:        TypeInt,
		Default:     "30",
		Min:         &minOne,
		Description: "How many days back to keep one backup per day.",
	})
	mustDeclare(Setting{
		Key:         "backup.keepWeekly",
		Area:        "backup",
		Type:        TypeInt,
		Default:     "12",
		Min:         &minOne,
		Description: "How many weeks back to keep one backup per week.",
	})
	mustDeclare(Setting{
		Key:         "backup.keepPreMigration",
		Area:        "backup",
		Type:        TypeInt,
		Default:     "5",
		Min:         &minOne,
		Description: "How many pre-migration backups to keep in their own directory.",
	})
	mustDeclare(Setting{
		Key:     "backup.verifyAfter",
		Area:    "backup",
		Type:    TypeBool,
		Default: "true",
		Description: "Verify a backup after every run (an independent open of the copy). " +
			"The default (true) is on: a verified backup is one that could be opened, " +
			"not a claim that it could be trusted.",
	})
	mustDeclare(Setting{
		Key:     "backup.staleAfter",
		Area:    "backup",
		Type:    TypeString,
		Default: "48h",
		Description: "How long since the last successful backup before the daemon reports " +
			"the backup state as stale. A duration such as 48h.",
	})
}
