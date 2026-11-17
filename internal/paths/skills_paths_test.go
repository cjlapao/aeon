package paths

import (
	"runtime"
	"testing"
)

// S65 T2 — the two skills roots: env override above the XDG chain, and
// the same absolute-or-ignore rule StoreDirFor set (I7: injected getenv,
// never a mutated process environment).

func env(kv map[string]string) func(string) string {
	return func(k string) string { return kv[k] }
}

func TestSkillsDirHonoursTheOverrideFirst(t *testing.T) {
	got, err := SkillsDirFor(env(map[string]string{
		SkillsDirEnv:   "/throwaway/skills",
		"XDG_DATA_HOME": "/xdg",
		"HOME":         "/home/dev",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if got != "/throwaway/skills" {
		t.Fatalf("override must win: %q", got)
	}
}

func TestSkillsDirFollowsXDGDataHome(t *testing.T) {
	if isWindows() {
		t.Skip("XDG is POSIX")
	}
	got, err := SkillsDirFor(env(map[string]string{
		"XDG_DATA_HOME": "/xdg",
		"HOME":          "/home/dev",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if want := "/xdg/aeon/skills"; got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestSkillsDirFallsBackToHome(t *testing.T) {
	if isWindows() {
		t.Skip("XDG is POSIX")
	}
	got, err := SkillsDirFor(env(map[string]string{"HOME": "/home/dev"}))
	if err != nil {
		t.Fatal(err)
	}
	if want := "/home/dev/.local/share/aeon/skills"; got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestSkillsDirIgnoresRelativeEnvValues(t *testing.T) {
	// A relative override would resolve against the daemon's working
	// directory — which is exactly the drift StoreDir's rule forbids.
	got, err := SkillsDirFor(env(map[string]string{
		SkillsDirEnv:   "relative/skills",
		"XDG_DATA_HOME": "relative-xdg",
		"HOME":         "/home/dev",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if want := "/home/dev/.local/share/aeon/skills"; got != want {
		t.Fatalf("relative values must be ignored; got %q want %q", got, want)
	}
}

func TestUserSkillsDirIsTheAgentsFolder(t *testing.T) {
	got, err := UserSkillsDirFor(env(map[string]string{"HOME": "/home/dev"}))
	if err != nil {
		t.Fatal(err)
	}
	if want := "/home/dev/.agents/skills"; got != want {
		t.Fatalf("the shared ecosystem folder is %q, got %q", want, got)
	}
	if got, _ := UserSkillsDirFor(env(map[string]string{
		UserSkillsDirEnv: "/fixture/skills", "HOME": "/home/dev",
	})); got != "/fixture/skills" {
		t.Fatalf("override must win: %q", got)
	}
}

func isWindows() bool { return runtime.GOOS == "windows" }
