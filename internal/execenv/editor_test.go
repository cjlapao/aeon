package execenv

import (
	"reflect"
	"strings"
	"testing"
)

// The editor child is the one spawn a CLI verb starts, and the one child whose
// allowlist was written for usability rather than for a model-driven command.
// These tests pin both halves: what it must carry (an editor is useless
// without them) and what it must never carry (the parent's credentials, which
// is the whole reason it is an allowlist and not os.Environ()).

func ambientEditorEnv() []string {
	return []string{
		"PATH=/usr/bin:/bin", "HOME=/home/cjlapao", "USER=cjlapao", "LOGNAME=cjlapao",
		"SHELL=/bin/bash", "LANG=en_GB.UTF-8", "LC_ALL=C.UTF-8", "TZ=Europe/London",
		"TMPDIR=/tmp",
		"TERM=xterm-256color", "COLORTERM=truecolor",
		"EDITOR=code -w", "VISUAL=code -w",
		"DISPLAY=:1", "WAYLAND_DISPLAY=wayland-0", "XDG_RUNTIME_DIR=/run/user/1000",
		// The things that must never ride into a child:
		"ANTHROPIC_API_KEY=sk-ant-leak", "AWS_SECRET_ACCESS_KEY=hunter2",
		"GH_TOKEN=ghp_leak", "DB_PASSWORD=p",
		"LD_PRELOAD=/tmp/evil.so", "SSH_AUTH_SOCK=/run/user/1000/ssh-agent",
		"AEON_CURSOR_SIGNING_KEY=abc",
		// Names that only LOOK like allowlist entries:
		"PATHOLOGY=1", "HOMEWORK=1", "TERM_PROGRAM=iTerm", "DISPLAY_ID=7", "EDITOR_CONFIG=x",
		// Junk:
		"NOEQUALSIGN", "=novalue", "",
	}
}

func namesOf(env []string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		out = append(out, strings.SplitN(kv, "=", 2)[0])
	}
	return out
}

func TestEditorEnvCarriesWhatAnEditorActuallyNeeds(t *testing.T) {
	got := namesOf(EditorEnv(ambientEditorEnv()))
	want := []string{
		"COLORTERM", "DISPLAY", "HOME", "LANG", "LC_ALL", "LOGNAME",
		"PATH", "SHELL", "TERM", "TMPDIR", "TZ", "USER", "WAYLAND_DISPLAY", "XDG_RUNTIME_DIR",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("the editor child carries %v, want exactly %v", got, want)
	}
	// And the values are the caller's, not invented.
	for _, kv := range EditorEnv(ambientEditorEnv()) {
		if kv == "TERM=" || kv == "PATH=" {
			t.Errorf("%q reached the child empty — the verb would launch an unusable editor", kv)
		}
	}
}

func TestEditorEnvIsAnAllowlistAndNotTheParentsEnvironment(t *testing.T) {
	text := strings.Join(EditorEnv(ambientEditorEnv()), "\n")
	for _, leak := range []string{
		"sk-ant-leak", "hunter2", "ghp_leak", "/tmp/evil.so",
		"ANTHROPIC_API_KEY", "AWS_SECRET_ACCESS_KEY", "GH_TOKEN", "DB_PASSWORD",
		"LD_PRELOAD", "SSH_AUTH_SOCK", "AEON_CURSOR_SIGNING_KEY",
	} {
		if strings.Contains(text, leak) {
			t.Errorf("the editor child carries %s — a credential-shaped or loader-shaped name never rides into a child, whatever the parent holds", leak)
		}
	}
}

func TestEditorEnvDoesNotMatchOnPrefixesItDoesNotOwn(t *testing.T) {
	for _, got := range namesOf(EditorEnv(ambientEditorEnv())) {
		for _, neighbour := range []string{"PATHOLOGY", "HOMEWORK", "TERM_PROGRAM", "DISPLAY_ID", "EDITOR_CONFIG"} {
			if got == neighbour {
				t.Errorf("%s was carried because it shares a prefix with an allowlist name — only LC_* is a prefix entry", neighbour)
			}
		}
	}
}

func TestEditorEnvIsSortedAndTheSameTwice(t *testing.T) {
	first := EditorEnv(ambientEditorEnv())
	second := EditorEnv(ambientEditorEnv())
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("two calls disagree: %v then %v — a child's environment must be reproducible", first, second)
	}
	names := namesOf(first)
	if !reflect.DeepEqual(names, sortedForTest(names)) {
		t.Errorf("the environment is not name-sorted: %v — a byte-pinned golden cannot be written against an unordered list", names)
	}
}

func TestEditorEnvDropsWhatItCannotParse(t *testing.T) {
	for _, kv := range EditorEnv(ambientEditorEnv()) {
		if !strings.Contains(kv, "=") {
			t.Errorf("%q reached the child without a value", kv)
		}
		if strings.HasPrefix(kv, "=") {
			t.Errorf("%q reached the child without a name", kv)
		}
	}
}

// TestEveryEditorNameEitherSurvivesOrIsNamed is the honest-states test. An
// allowlist name the deny list swallows does not fail: it just stops being
// there, and the verb breaks in a way nobody can read off a refusal. So the two
// closed lists are compared directly, and any name the deny list refuses has to
// appear in the named exception set — a silent widening is exactly what EV4
// forbids, and a widening nobody can see is how one happens.
func TestEveryEditorNameEitherSurvivesOrIsNamed(t *testing.T) {
	deny := Deny()
	for _, n := range append(EditorNames(), DefaultPassthrough()...) {
		if strings.HasSuffix(n, "*") {
			continue
		}
		rule := deniedBy(deny, n, false)
		if rule == "" {
			continue
		}
		if !editorDenyExceptions[n] {
			t.Errorf("the editor allowlist names %s and the deny list refuses it (%s) — it would vanish from the child with no word said; either drop the name or name the widening in editorDenyExceptions", n, rule)
		}
	}
}

// TestTheEditorWideningIsOnlyThePersonsSession is EV4's bite. The exception set
// exists because EV1's scope is every spawn a MODEL can reach, and this child
// is the person's own editor at the person's own terminal — the same recorded
// class as the pty shell. That argument covers the session handles and nothing
// else, so no name refused as a credential or a loader variable may join it.
func TestTheEditorWideningIsOnlyThePersonsSession(t *testing.T) {
	deny := Deny()
	for _, n := range EditorDenyExceptions() {
		rule := deniedBy(deny, n, false)
		if rule == "" {
			t.Errorf("%s is in the exception set but the deny list does not refuse it — the entry is dead weight that reads like a widening", n)
			continue
		}
		if strings.Contains(rule, "credential word") || strings.Contains(rule, "loader/shell-injection") {
			t.Errorf("%s is excepted from the deny list on the strength of %q — only the person's session handles may be excepted, never a credential or a loader name", n, rule)
		}
		if !strings.Contains(rule, "the exact name") {
			t.Errorf("%s is excepted on %q, which is not the exact-name refusal the exception was argued for — re-argue it or drop it", n, rule)
		}
	}
	// And the widening is closed: a name outside the allowlist cannot be
	// excepted into the child by joining the map alone.
	for n := range editorDenyExceptions {
		found := false
		for _, e := range EditorNames() {
			if e == n {
				found = true
			}
		}
		if !found {
			t.Errorf("%s is excepted but not on the editor allowlist — an exception with no allowlist entry is a way to add a name without arguing it in", n)
		}
	}
}

// TestEditorEnvNeverCarriesTheEditorChoice is the second half of the same
// argument: the verb resolved $VISUAL/$EDITOR into argv before it spawned, so
// the child does not need them — and a child that can re-resolve them can pick
// an editor nobody chose. `EDITOR` is in the deny list's loader family for
// precisely that reason.
func TestEditorEnvNeverCarriesTheEditorChoice(t *testing.T) {
	for _, got := range namesOf(EditorEnv(ambientEditorEnv())) {
		if got == "EDITOR" || got == "VISUAL" {
			t.Errorf("%s reached the editor child — the program is chosen in argv, not by the child", got)
		}
	}
	for _, n := range EditorNames() {
		if n == "EDITOR" || n == "VISUAL" {
			t.Errorf("%s is on the editor allowlist, which reopens the choice the verb already made", n)
		}
	}
}

func sortedForTest(in []string) []string {
	out := append([]string(nil), in...)
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1] > out[j]; j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out
}
