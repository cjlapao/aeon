package skills

import "time"

// Status is the validation verdict for one discovered skill — three states,
// never two (P31). A skill nobody can validate is INVALID with named issues,
// not silently absent; a skill that reads fine but ships a smell is WARN,
// still offered, with the sentence attached.
type Status string

const (
	StatusValid   Status = "valid"
	StatusWarn    Status = "warn"
	StatusInvalid Status = "invalid"
)

// Source is which root a skill was discovered under. The user root is
// theirs — Aeon lists it and never writes to it; the managed root is the
// app's own, and the only place Install may write.
type Source string

const (
	SourceUser    Source = "user"
	SourceManaged Source = "managed"
)

// Severity splits issues the same three-state way: an error makes a skill
// invalid (the spec is broken), a warning keeps it valid-or-warn (the skill
// reads but smells). The words are wire values; the bench renders three
// pills from them plus the shadowed marker.
type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
)

// Issue is one finding against one skill: the field it concerns, the
// sentence naming what and why, and its severity. Refusals and findings
// carry the field because "SKILL.md is bad" is not actionable and
// "description is 1400 characters; the spec caps it at 1024" is (DB19's
// discipline, applied to validation rather than infrastructure).
type Issue struct {
	Field    string   `json:"field"`
	Detail   string   `json:"detail"`
	Severity Severity `json:"severity"`
}

// FileCounts is the skill folder's manifest: how many files sit under the
// spec's three conventional directories and how many elsewhere. Counts
// only, never names — the bench shows WHAT KIND of thing a skill ships
// (does it bundle scripts? references?) without pretending to a file
// browser, and the install caps are enforced against exactly these walks.
type FileCounts struct {
	Scripts    int `json:"scripts"`
	References int `json:"references"`
	Assets     int `json:"assets"`
	Other      int `json:"other"`
}

// Skill is one discovered skill folder, as the catalog answers it. The
// fields are the frontmatter the spec defines plus the facts discovery
// measured (where it came from, how big it is, what it ships, how it
// validated). Unknown frontmatter keys ride in Unknown rather than being
// dropped: Claude Code and Codex each extend the format with their own
// fields, and a loader that refuses a field it does not know turns every
// future spec addition into a missing skill.
type Skill struct {
	// Name is the folder name — the identity on disk. The spec binds
	// frontmatter name to it; validation enforces the binding, and the
	// catalog keys on the folder name either way so an invalid skill is
	// still listable under a stable key.
	Name string `json:"name"`
	// FrontmatterName is what the file CLAIMS, kept even when it disagrees
	// with Name (that disagreement is an issue, not a rewrite).
	FrontmatterName string            `json:"frontmatterName,omitempty"`
	Description     string            `json:"description,omitempty"`
	License         string            `json:"license,omitempty"`
	Compatibility   string            `json:"compatibility,omitempty"`
	AllowedTools    string            `json:"allowedTools,omitempty"`
	Metadata        map[string]string `json:"metadata,omitempty"`
	Unknown         map[string]string `json:"unknown,omitempty"`

	Status Status  `json:"status"`
	Issues []Issue `json:"issues,omitempty"`

	Source Source `json:"source"`
	Root   string `json:"root"` // the scanned root directory
	Path   string `json:"path"` // the skill folder itself

	Files      FileCounts `json:"files"`
	BodyBytes  int        `json:"bodyBytes"`
	BodyLines  int        `json:"bodyLines"`
	TotalBytes int64      `json:"totalBytes"`

	// Shadowed records that another root carried a same-named skill and
	// won (the user root outranks the managed one, the order Discover was
	// given is the precedence). The loser stays LISTABLE — deleting the
	// fact that a second copy exists is how a person loses an hour
	// wondering which file they edited.
	Shadowed   bool   `json:"shadowed,omitempty"`
	ShadowedBy string `json:"shadowedBy,omitempty"`

	ScannedAt time.Time `json:"scannedAt"`
}

// GetResult is skills.get's answer: the catalog view plus the SKILL.md body
// as stored, truncated at BodyCapBytes with the flag raised rather than the
// text silently shortened (a preview that lies about being complete is the
// confident wrong answer P31 names).
type GetResult struct {
	Skill     *Skill `json:"skill"`
	Body      string `json:"body"`
	Truncated bool   `json:"truncated,omitempty"`
}

// Summary counts a scan's verdicts — the number the bench header and the
// CLI line print, and the number `skills.validate` answers with.
type Summary struct {
	Valid    int `json:"valid"`
	Warn     int `json:"warn"`
	Invalid  int `json:"invalid"`
	Shadowed int `json:"shadowed"`
}

// ScanResult is one catalog scan: every skill from every root in precedence
// order, the roots themselves (so the bench can say where it looked), and
// the clock time the scan ran.
type ScanResult struct {
	Skills      []*Skill  `json:"skills"`
	UserRoot    string    `json:"userRoot"`
	ManagedRoot string    `json:"managedRoot"`
	ScannedAt   time.Time `json:"scannedAt"`
	Summary     Summary   `json:"summary"`
	// Watching is S66's honest third state of the file watcher: true
	// means a dropped-in folder is noticed within the debounce window;
	// false means it is not, and WatchError says why. A cache served by
	// a blind daemon must never look like a watched one.
	Watching   bool   `json:"watching"`
	WatchError string `json:"watchError,omitempty"`
}

// Limits bounds discovery so a hostile or broken tree cannot turn a catalog
// scan into an incident. The numbers are generous against real skills
// (skill-creator, the largest here, ships dozens of files and hundreds of
// KiB) and hostile against anything else.
type Limits struct {
	EntriesPerRoot int   // skill folders one root may offer
	FilesPerSkill  int   // files one skill folder may contain
	BytesPerSkill  int64 // total bytes one skill folder may hold
	SkillMdMax     int64 // SKILL.md itself
	BodyMax        int64 // body returned by Get
}

// DefaultLimits is the shipped bound. Exceeding any of them does not crash
// the scan: the offending skill is reported (an issue, or the folder
// skipped with a named issue) and the scan goes on — one oversized folder
// may never hide every other skill in its root.
func DefaultLimits() Limits {
	return Limits{
		EntriesPerRoot: 2000,
		FilesPerSkill:  2000,
		BytesPerSkill:  64 << 20,
		SkillMdMax:     1 << 20,
		BodyMax:        256 << 10,
	}
}
