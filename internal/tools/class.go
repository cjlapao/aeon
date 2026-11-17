package tools

import "sort"

// RerunClass says what a crash recovery may do when a tool_use has no saved
// tool_result. The empty value is invalid so a new tool cannot default safe.
type RerunClass string

const (
	RerunSafe       RerunClass = "safe"
	RerunDetectable RerunClass = "detectable"
	RerunUnsafe     RerunClass = "unsafe"
)

func (c RerunClass) Valid() bool {
	return c == RerunSafe || c == RerunDetectable || c == RerunUnsafe
}

// RerunClasses returns the closed vocabulary of re-run classes, sorted — the
// list `tools.vocabulary` sends so a screen can offer the classes as filter
// options without holding its own copy of the words (S47 T10, TC3's rule about
// who owns a vocabulary). `TestRerunClassesMatchCommittedList` already pins
// these three spellings against the committed `rerun_classes.json`; this is the
// same closed set seen as data, and a fourth class would have to arrive here,
// in the constants above and in the golden, or a test says so.
func RerunClasses() []RerunClass {
	out := []RerunClass{RerunSafe, RerunDetectable, RerunUnsafe}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
