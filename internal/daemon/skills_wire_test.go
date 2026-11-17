package daemon

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/runtime"
	"github.com/cjlapao/aeon/internal/skills"
)

// S66 T2/T3. The renderer is the composition point — its words are the
// transcript's words and the chat's words — so the words themselves are
// pinned here, not paraphrased from the plan.

func skillDirFixture(t *testing.T, dir, name, desc string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "---\nname: " + name + "\ndescription: " + desc + "\n---\n\nbody\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestTheSkillsProviderWiringAppearsInExactlyOnePlace is the S66 twin of
// the policy canonicalizer's grep pin: the seam is DEFINED once
// (runtime/skills_inject.go) and CALLED once in production
// (daemon/skills_wire.go, from New). A second caller is a second place
// that can decide what every model sees, wearing an init-time seam's
// name; test files are exempt because they set the hook on purpose.
func TestTheSkillsProviderWiringAppearsInExactlyOnePlace(t *testing.T) {
	const seamExpr = "SetSkillsProvider("
	root := repoRootForTest(t)
	var defs, calls int
	var defFiles, callFiles []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			if info != nil && info.IsDir() {
				name := info.Name()
				if name == "ui" || name == ".git" || name == "node_modules" || name == ".tmp" {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		text, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		d := strings.Count(string(text), "func "+seamExpr)
		c := strings.Count(string(text), seamExpr) - d
		if d != 0 {
			defs += d
			defFiles = append(defFiles, path)
		}
		if c != 0 {
			calls += c
			callFiles = append(callFiles, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if defs != 1 || !strings.HasSuffix(defFiles[0], filepath.Join("internal", "runtime", "skills_inject.go")) {
		t.Fatalf("SetSkillsProvider is defined %d times in %v; want exactly once in runtime/skills_inject.go", defs, defFiles)
	}
	if calls != 1 || !strings.HasSuffix(callFiles[0], filepath.Join("internal", "daemon", "skills_wire.go")) {
		t.Fatalf("SetSkillsProvider is called %d times in %v; want exactly once in daemon/skills_wire.go", calls, callFiles)
	}
}

func repoRootForTest(t *testing.T) string {
	t.Helper()
	// tests run in internal/daemon; the module root is two up.
	abs, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

func TestSectionOffersOnlyWhatWouldRun(t *testing.T) {
	// Three shapes: one valid, one invalid (broken frontmatter), one
	// shadowed (user hides managed). The model sees the valid one only.
	res := &skills.ScanResult{Skills: []*skills.Skill{
		{Name: "docx", Description: "Word work.\nSecond line nobody sees.", Status: skills.StatusValid, Path: "/u/docx"},
		{Name: "thin", Description: "Thin but usable.", Status: skills.StatusWarn, Path: "/u/thin"},
		{Name: "broken", Description: "x", Status: skills.StatusInvalid, Path: "/u/broken"},
		{Name: "hidden", Description: "shadowed work", Status: skills.StatusValid, Path: "/m/hidden", Shadowed: true},
	}}
	sec := skillsSectionFrom(res)
	if sec == nil {
		t.Fatal("a catalog with usable skills makes an offer")
	}
	if sec.Count != 2 || sec.Names[0] != "docx" || sec.Names[1] != "thin" {
		t.Fatalf("invalid and shadowed entries leaked into the offer: %+v", sec)
	}
	if strings.Contains(sec.Text, "broken") || strings.Contains(sec.Text, "hidden") {
		t.Fatalf("the section named what cannot run:\n%s", sec.Text)
	}
	if !strings.Contains(sec.Text, "DATA") || !strings.Contains(sec.Text, "read tool") {
		t.Fatal("the INJ2 framing (descriptions are data; read before following) must be IN the section")
	}
	if strings.Contains(sec.Text, "Second line") {
		t.Fatal("descriptions flatten to one line — the section is an index")
	}
	if !strings.Contains(sec.Text, "/u/docx/SKILL.md") {
		t.Fatal("the path to SKILL.md is the whole point of the row")
	}
	if sec.Marker != "skills injected: 2 — docx, thin" {
		t.Fatalf("marker: %q", sec.Marker)
	}
}

func TestSectionCapsAreEnforcedAndSpoken(t *testing.T) {
	res := &skills.ScanResult{}
	for i := 0; i < 70; i++ {
		res.Skills = append(res.Skills, &skills.Skill{
			Name:        "sk" + strings.Repeat("x", 0) + string(rune('a'+i%26)) + itoaTest(i),
			Description: "A description of moderate length for cap maths.",
			Status:      skills.StatusValid,
			Path:        "/u/skill",
		})
	}
	sec := skillsSectionFrom(res)
	if sec.Count != skillsInjectMaxEntries || !sec.Truncated {
		t.Fatalf("the entry cap must bite at %d and say so, got count=%d truncated=%v",
			skillsInjectMaxEntries, sec.Count, sec.Truncated)
	}
	if !strings.Contains(sec.Text, "cut at the cap") || !strings.Contains(sec.Marker, "(+more)") {
		t.Fatal("a truncation nobody can see is a lie of omission")
	}
	// The byte cap: maximal descriptions, every line at the width limit.
	wide := &skills.ScanResult{}
	for i := 0; i < skillsInjectMaxEntries; i++ {
		wide.Skills = append(wide.Skills, &skills.Skill{
			Name:        strings.Repeat("n", 30) + itoaTest(i),
			Description: strings.Repeat("d", 1024),
			Status:      skills.StatusValid,
			Path:        "/u/skill",
		})
	}
	w := skillsSectionFrom(wide)
	// Text carries framing + rows + truncation line; the rows themselves
	// are what the byte cap governs.
	if !w.Truncated {
		t.Fatalf("32 KiB of descriptions must be truncated, got %d bytes over %d rows", len(w.Text), w.Count)
	}
	if len(w.Text) > skillsInjectMaxBytes+2048 {
		t.Fatalf("the byte cap overshot: %d", len(w.Text))
	}
}

func TestProviderFaultInjectsNothingAndSpeaks(t *testing.T) {
	w := &wire{}
	fault := faultScanner{err: context.DeadlineExceeded}
	provider := skillsProviderFor(fault, w)
	if sec := provider(); sec != nil {
		t.Fatal("a catalog that cannot be read makes no offer")
	}
	joined := strings.Join(w.logs, "\n")
	if !strings.Contains(joined, "inject skipped") {
		t.Fatalf("the fault must be spoken in the daemon's log, got: %s", joined)
	}
}

type faultScanner struct{ err error }

func (f faultScanner) Scan(bool) (*skills.ScanResult, error) { return nil, f.err }

func TestNewWiresTheProviderAndUnwiredLeavesItInert(t *testing.T) {
	// The walk found this the hard way: a wireSkills that EXISTS but is
	// never CALLED still passes the one-place guard (the guard counts
	// SetSkillsProvider's call site, not wireSkills's). What the model
	// sees depends on New actually handing the catalog over.
	t.Cleanup(func() { runtime.SetSkillsProvider(nil) })
	user := t.TempDir()
	d := filepath.Join(user, "docx")
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "SKILL.md"),
		[]byte("---\nname: docx\ndescription: Word work.\n---\n\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	svc, err := skills.NewService(skills.Options{
		Getenv: func(k string) string {
			if k == "AEON_USER_SKILLS_DIR" {
				return user
			}
			return ""
		},
		Clock: time.Now,
	})
	if err != nil {
		t.Fatal(err)
	}
	w := &wire{}
	New(Options{DaemonVersion: "9.9.9-test", BuildTime: "x", GitCommit: "y",
		Logger: w, Skills: svc})
	direct, derr := svc.Scan(true)
	if derr != nil {
		t.Fatal(derr)
	}
	t.Logf("direct scan: %d skills; %d valid", len(direct.Skills), direct.Summary.Valid)
	for _, sk := range direct.Skills {
		t.Logf("  %s status=%s shadowed=%v", sk.Name, sk.Status, sk.Shadowed)
	}
	provider := runtime.TheSkillsProvider()
	if provider == nil {
		t.Fatal("New with a catalog left the runtime seam nil — the catalog would never reach a model")
	}
	sec := provider()
	if sec == nil || sec.Count != 1 || sec.Names[0] != "docx" {
		t.Fatalf("the wired provider must answer the catalog, got %+v; logs: %v", sec, w.logs)
	}
	// And the other mount: a daemon built WITHOUT a catalog must leave the
	// seam as it found it (V13 — absence is inert, not stale).
	runtime.SetSkillsProvider(nil)
	New(Options{DaemonVersion: "9.9.9-test", BuildTime: "x", GitCommit: "y", Logger: &wire{}})
	if runtime.TheSkillsProvider() != nil {
		t.Fatal("a no-catalog mount wired a provider anyway")
	}
}

func TestSkillsChangedReachesASubscriber(t *testing.T) {
	// The full door: a folder lands in a watched root, the daemon's own
	// refresh announces it, and a subscribed client receives
	// `skills.changed` with the name — no restart, no re-poll.
	user, managed := t.TempDir(), t.TempDir()
	svc, err := skills.NewService(skills.Options{
		Getenv: func(k string) string {
			switch k {
			case "AEON_USER_SKILLS_DIR":
				return user
			case "AEON_SKILLS_DIR":
				return managed
			}
			return ""
		},
		Clock:         time.Now,
		WatchDebounce: time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	w := &wire{}
	d := New(Options{
		DaemonVersion: "9.9.9-test", BuildTime: "2026-10-31T00:00:00Z", GitCommit: "deadbee",
		Logger: w, Notify: w.notify, Skills: svc,
	})
	ctx := context.Background()
	initConn(t, d, ctx, "c1", "watcher")
	subscribe(t, d, "c1", protocol.SubscribeParams{})

	if err := svc.StartWatch(ctx); err != nil {
		t.Fatalf("StartWatch: %v", err)
	}
	skillDirFixture(t, filepath.Join(user, "docx"), "docx", "Word work.")
	svc.RecheckNow() // deterministic: the watcher's OWN refresh path, no timing race

	var payload map[string]any
	for _, s := range w.delivered() {
		if s.Event.Kind == protocol.KindSkillsChanged {
			raw, _ := json.Marshal(s.Event.Payload)
			if err := json.Unmarshal(raw, &payload); err != nil {
				t.Fatalf("payload does not decode: %v", err)
			}
		}
	}
	if payload == nil {
		t.Fatal("skills.changed never reached the subscriber")
	}
	added, _ := payload["added"].([]any)
	if len(added) != 1 || added[0] != "docx" {
		t.Fatalf("the announced diff is wrong: %v", payload)
	}
	if payload["watching"] != true {
		t.Fatalf("a watched daemon must say so in the event: %v", payload)
	}
	// Nothing new ⇒ nothing announced: the second refresh is silent.
	before := len(w.delivered())
	svc.RecheckNow()
	for _, s := range w.delivered()[before:] {
		if s.Event.Kind == protocol.KindSkillsChanged {
			t.Fatalf("a refresh that found no diff must announce nothing: %+v", s.Event)
		}
	}
	// And the chat's read-back answers the same truth: the scan the event
	// mirrored is the service's cached state.
	res, err := svc.Scan(false)
	if err != nil || !res.Watching {
		t.Fatalf("the daemon that just watched must report watching: %+v %v", res, err)
	}
}

func itoaTest(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
