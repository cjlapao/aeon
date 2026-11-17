package config

// settings_decl_gates.go — S57 T7's two post-merge watch settings (VG5).
// The watch window is how long a merged PR is kept under eye after the
// landing; the poll interval is how often the watcher reads the due rows.
// Both are TypeString on purpose: a duration is a canonical text form
// ("2h", "30 s"), read live per tick and parsed at the read site — the
// watcher keeps polling on a value that does not parse (its log says so),
// so a mistyped setting is a loud degraded default, never a stopped watch.
func init() {
	mustDeclare(Setting{
		Key:  "gates.watchWindow",
		Area: "gates",
		Type: TypeString,
		// The canonical text form the value takes.
		Default:     "2h",
		Description: "How long a merged PR is kept under the post-merge watch before an unanswered watch expires. A duration such as 30m or 2h; a value that does not parse falls back to the 2h default, which is logged.",
	})
	mustDeclare(Setting{
		Key:         "gates.watchInterval",
		Area:        "gates",
		Type:        TypeString,
		Default:     "30s",
		Description: "How often the post-merge watcher reads its due rows. A duration such as 15s or 30s; a value that does not parse falls back to the 30s default, which is logged.",
	})
}
