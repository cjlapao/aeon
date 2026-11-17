package daemon

// s77_launch_attachments_test.go — a loop may START with files attached
// (S77). The rules tested here are the ones the plan argued for:
//
//   - an attachment is RESOLVED against the start's own {slug, workspaceRoot}
//     before any row exists, and a launch that names attachments without
//     naming the slug they were put under is refused at the door;
//   - the start's ROUTE must declare the capability each type asks — an
//     undeclared capability is not a silent yes, and the refusal names the
//     route, the capability and the file, writing nothing;
//   - the launch's kickoff row carries the store's receipts, run 1 links
//     that row, and the receipts reach the step's wire as content blocks
//     through the ONE expansion point;
//   - a restart (the forget door) re-rides from the LINKED kickoff row — and
//     only from it: a later turn's message is never mistaken for the launch.

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/loop"
	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

var s77PNG = []byte("\x89PNG\r\n\x1a\n" + "launch-bytes")

// s77Fixture is gatedFixture with one difference the S77 tests need: the
// route's DECLARED capabilities. gatedFixture's route declares none, which
// is exactly the refusal case; the rides-the-launch cases need a route that
// declares what the bytes ask.
func s77Fixture(t *testing.T, turns []gatedTurn, caps []string) (*streamFixture, *gatedModel) {
	t.Helper()
	s := recoveryStore(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("hello streaming\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	seedRules(t, s, time.Now().UTC())
	w := &wire{}
	route := model.Route{ID: "r1", Provider: "anthropic", Model: "m", MaxOutputTokens: 1024, Capabilities: caps}
	m := &gatedModel{turns: turns}
	d := New(Options{
		Store: s, Routes: protocol.NewStaticRoutes(route),
		Models: m, Logger: w, Notify: w.notify,
	})
	t.Cleanup(func() { d.Table.WaitContinuations() })
	return &streamFixture{d: d, w: w, s: s, root: root, route: route}, m
}

// s77Put stages bytes under the GIVEN slug — the launch case's whole point is
// that the put happens BEFORE the loop row exists, so the fixture cannot
// reuse s69Put's fixed "chat" slug.
func s77Put(t *testing.T, f *streamFixture, conn, slug, name, mediaType string, data []byte) map[string]any {
	t.Helper()
	res := dispatchJSON(t, f, conn, protocol.MethodAttachmentPut, map[string]any{
		"slug": slug, "componentRoot": f.root, "name": name,
		"mediaType": mediaType, "data": base64.StdEncoding.EncodeToString(data),
	})
	rec, ok := res["receipt"].(map[string]any)
	if !ok {
		t.Fatalf("no receipt came back: %v", res)
	}
	return rec
}

func s77Start(t *testing.T, f *streamFixture, params map[string]any) map[string]any {
	t.Helper()
	bp := writeStreamBlueprint(t, "read-only", "go")
	params["blueprint"] = bp
	if _, ok := params["workspaceRoot"]; !ok {
		params["workspaceRoot"] = f.root
	}
	res := dispatchJSON(t, f, "c-init", protocol.MethodLoopStart, params)
	if _, ok := res["id"].(string); !ok {
		t.Fatalf("start returned no loop id: %#v", res)
	}
	return res
}

func s77LoopCount(t *testing.T, f *streamFixture) int {
	t.Helper()
	var n int
	if err := f.s.GormDB().Raw(`SELECT COUNT(*) FROM loop`).Row().Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func s77ImageBlocks(msgs []model.Message) []model.Block {
	var out []model.Block
	for _, msg := range msgs {
		for _, b := range msg.Content {
			if b.Kind == model.BlockImage {
				out = append(out, b)
			}
		}
	}
	return out
}

// The happy path: bytes put under a minted slug, the start naming that slug,
// the kickoff row filing the receipts, run 1 linking the row, and the image
// block riding the launch's first step through the ONE expansion point.
func TestALaunchWithAttachmentsFilesReceiptsAndRidesTheFirstStep(t *testing.T) {
	f, m := s77Fixture(t, []gatedTurn{{deltas: []string{turnOK}}},
		[]string{"text", "image", "tool-calling", "pdf", "file"})
	initConn(t, f.d, context.Background(), "c-init", "cli")
	rec := s77Put(t, f, "c-init", "launch-shot", "diagram.png", "image/png", s77PNG)
	doc := rec["document"].(string)

	s77Start(t, f, map[string]any{
		"goal": "look at the shot", "route": "r1", "slug": "launch-shot",
		"attachments": []any{map[string]any{"document": doc, "rev": 1}},
	})
	waitFor(t, "the launch's model call", func() bool { return m.callCount() >= 1 })
	settled := waitLoopSettled(t, f, loopIDOf(t, f))

	// The kickoff row: human, carrying the goal words AND the store's receipt.
	sc := store.Scope{LoopSlug: "launch-shot", ComponentRoot: f.root}
	rows, err := f.s.Thread(sc, 0)
	if err != nil {
		t.Fatal(err)
	}
	var kickoff *store.ChatMessage
	for i := range rows {
		if rows[i].Author == "human" {
			kickoff = &rows[i]
		}
	}
	if kickoff == nil || kickoff.Body != "look at the shot" || len(kickoff.Attachments) != 1 {
		t.Fatalf("the kickoff row did not carry the receipts: %+v", rows)
	}
	r := kickoff.Attachments[0]
	if r.Document != doc || r.Rev != 1 || r.MediaType != "image/png" || r.Name != "diagram.png" || r.Size != len(s77PNG) {
		t.Fatalf("the filed receipt is not the store's answer: %+v", r)
	}
	// Run 1 LINKS the kickoff row — bytes bind to a linked fact, not to
	// thread order.
	run, err := f.s.CurrentRun(settled.Loop.ID)
	if err != nil {
		t.Fatal(err)
	}
	if run.KickoffMessageID == nil || *run.KickoffMessageID != kickoff.ID {
		t.Fatalf("run 1 does not link the launch kickoff: %v", run.KickoffMessageID)
	}

	// The WIRE: the image block rode the launch's step, expanded at the one
	// compile boundary, bytes intact.
	blocks := s77ImageBlocks(m.lastSent(0))
	if len(blocks) != 1 {
		t.Fatalf("the image did not ride the launch's first step: %d image blocks", len(blocks))
	}
	back, derr := base64.StdEncoding.DecodeString(blocks[0].Data)
	if derr != nil || string(back) != string(s77PNG) {
		t.Fatalf("the expansion lost bytes: %q", blocks[0].Data)
	}
}

// The route rule: an image asked of a route that does not DECLARE "image"
// refuses the WHOLE launch — naming route, capability and file — and no loop
// row exists afterwards.
func TestALaunchRefusesAttachmentsItsRouteCannotCarry(t *testing.T) {
	f, _ := s77Fixture(t, nil, nil) // the pre-declaration shape: no capabilities at all
	initConn(t, f.d, context.Background(), "c-init", "cli")
	rec := s77Put(t, f, "c-init", "launch-shot", "diagram.png", "image/png", s77PNG)
	doc := rec["document"].(string)

	bp := writeStreamBlueprint(t, "read-only", "go")
	errRes := dispatchErr(t, f, "c-init", protocol.MethodLoopStart, map[string]any{
		"blueprint": bp, "goal": "look at the shot", "workspaceRoot": f.root, "route": "r1", "slug": "launch-shot",
		"attachments": []any{map[string]any{"document": doc, "rev": 1}},
	})
	for _, want := range []string{"diagram.png", "image", "r1"} {
		if !strings.Contains(errRes, want) {
			t.Fatalf("the capability refusal did not name %q: %q", want, errRes)
		}
	}
	if n := s77LoopCount(t, f); n != 0 {
		t.Fatalf("a refused launch wrote %d loop rows; nothing-half-written means 0", n)
	}
}

// An image-only route carries the picture but NOT the text file (which asks
// "file"): one offender poisons the call, and the sentence names exactly
// which file asked what.
func TestAPartiallyCapableRouteRefusesNamingTheOffender(t *testing.T) {
	f, _ := s77Fixture(t, nil, []string{"text", "image"})
	initConn(t, f.d, context.Background(), "c-init", "cli")
	img := s77Put(t, f, "c-init", "launch-shot", "diagram.png", "image/png", s77PNG)
	doc := s77Put(t, f, "c-init", "launch-shot", "notes.md", "text/plain", []byte("# launch notes\n"))

	bp := writeStreamBlueprint(t, "read-only", "go")
	errRes := dispatchErr(t, f, "c-init", protocol.MethodLoopStart, map[string]any{
		"blueprint": bp, "goal": "two files", "workspaceRoot": f.root, "route": "r1", "slug": "launch-shot",
		"attachments": []any{
			map[string]any{"document": img["document"], "rev": 1},
			map[string]any{"document": doc["document"], "rev": 1},
		},
	})
	if !strings.Contains(errRes, "notes.md") || !strings.Contains(errRes, `"file"`) {
		t.Fatalf("the offender was not named: %q", errRes)
	}
	if n := s77LoopCount(t, f); n != 0 {
		t.Fatalf("a refused launch wrote %d loop rows", n)
	}
}

// The door rule the plan argued for: bytes can only be put under a slug that
// EXISTS before the start, so a start that attaches without naming one is
// refused rather than resolved against a minted slug that owns no puts.
func TestStartWithAttachmentsAndNoSlugRefusesAtTheDoor(t *testing.T) {
	f, _ := s77Fixture(t, nil, []string{"text", "image"})
	initConn(t, f.d, context.Background(), "c-init", "cli")
	bp := writeStreamBlueprint(t, "read-only", "go")
	errRes := dispatchErr(t, f, "c-init", protocol.MethodLoopStart, map[string]any{
		"blueprint": bp, "goal": "orphan bytes", "workspaceRoot": f.root, "route": "r1",
		"attachments": []any{map[string]any{"document": "@attachments/00000000-x.png", "rev": 1}},
	})
	if !strings.Contains(errRes, "explicit `slug`") {
		t.Fatalf("the missing-slug refusal did not name the remedy: %q", errRes)
	}
	if n := s77LoopCount(t, f); n != 0 {
		t.Fatalf("the refused start wrote %d loop rows", n)
	}
}

// A reference that was never put under THIS scope refuses naming it — the
// same ResolveAttachmentRefs sentence `loop.message` says (one rule, no
// launch-shaped fork).
func TestStartAttachmentsResolveAgainstThePutScope(t *testing.T) {
	f, _ := s77Fixture(t, nil, []string{"text", "image"})
	initConn(t, f.d, context.Background(), "c-init", "cli")
	bp := writeStreamBlueprint(t, "read-only", "go")
	errRes := dispatchErr(t, f, "c-init", protocol.MethodLoopStart, map[string]any{
		"blueprint": bp, "goal": "stale ref", "workspaceRoot": f.root, "route": "r1", "slug": "launch-shot",
		"attachments": []any{map[string]any{"document": "@attachments/deadbeef-x.png", "rev": 3}},
	})
	if !strings.Contains(errRes, "never put") {
		t.Fatalf("the never-put refusal did not speak: %q", errRes)
	}
	if n := s77LoopCount(t, f); n != 0 {
		t.Fatalf("the refused start wrote %d loop rows", n)
	}
}

// The restart shape: forgetLaunchAttachments stands for a daemon coming back
// up; the receipts re-ride from the LINKED kickoff row. And the byte-
// identity case: a plain start caches the ABSENCE, so every step of a plain
// loop never reads the thread again.
func TestLaunchReceiptsRerideFromTheLinkedKickoffAfterRestart(t *testing.T) {
	f, m := s77Fixture(t, []gatedTurn{{deltas: []string{turnOK}}, {deltas: []string{turnOK}}},
		[]string{"text", "image", "tool-calling", "pdf", "file"})
	initConn(t, f.d, context.Background(), "c-init", "cli")
	rec := s77Put(t, f, "c-init", "launch-shot", "diagram.png", "image/png", s77PNG)
	doc := rec["document"].(string)
	s77Start(t, f, map[string]any{
		"goal": "persist me", "route": "r1", "slug": "launch-shot",
		"attachments": []any{map[string]any{"document": doc, "rev": 1}},
	})
	waitFor(t, "the launch's model call", func() bool { return m.callCount() >= 1 })
	settled := waitLoopSettled(t, f, loopIDOf(t, f))

	st, err := loop.Pinned(context.Background(), f.s, settled.Loop.ID)
	if err != nil {
		t.Fatal(err)
	}
	f.d.forgetLaunchAttachments(settled.Loop.ID)
	reloaded := f.d.launchAttachmentsFor(st)
	if len(reloaded) != 1 || reloaded[0].Document != doc || reloaded[0].Rev != 1 {
		t.Fatalf("the receipts did not re-ride from the linked kickoff: %+v", reloaded)
	}
}

// The narrower-read rule: a loop started plain, whose LATER message row
// carries attachments, must NOT have those attachments re-told as the
// launch's after a restart — run 1 links nothing, and no link is no
// receipts. (The chosen-skills thread-order fallback stays chosen-skills'
// own; bytes do not lean on it.)
func TestATurnAttachmentIsNeverMistakenForTheLaunch(t *testing.T) {
	f, m := s77Fixture(t, []gatedTurn{{deltas: []string{turnOK}}, {deltas: []string{turnOK}}},
		[]string{"text", "image", "tool-calling", "pdf", "file"})
	initConn(t, f.d, context.Background(), "c-init", "cli")
	rec := s77Put(t, f, "c-init", "plain-launch", "diagram.png", "image/png", s77PNG)
	doc := rec["document"].(string)
	s77Start(t, f, map[string]any{"goal": "plain start", "route": "r1", "slug": "plain-launch"})
	waitFor(t, "the launch's model call", func() bool { return m.callCount() >= 1 })
	settled := waitLoopSettled(t, f, loopIDOf(t, f))

	// A later turn files a receipt on its OWN row — a turn, not a launch.
	sc := store.Scope{LoopSlug: "plain-launch", ComponentRoot: f.root}
	if _, err := f.s.Note(sc, store.NewChatMessage{Author: "human", Kind: "note", Body: "a later turn",
		Attachments: []store.AttachmentReceipt{{Name: "diagram.png", Document: doc, Rev: 1, MediaType: "image/png", Size: len(s77PNG)}}}); err != nil {
		t.Fatal(err)
	}
	st, err := loop.Pinned(context.Background(), f.s, settled.Loop.ID)
	if err != nil {
		t.Fatal(err)
	}
	f.d.forgetLaunchAttachments(settled.Loop.ID)
	if got := f.d.launchAttachmentsFor(st); len(got) != 0 {
		t.Fatalf("a later turn's attachment was mistaken for the launch's: %+v", got)
	}
}

// loopIDOf reads back the single loop this fixture started.
func loopIDOf(t *testing.T, f *streamFixture) string {
	t.Helper()
	var id string
	if err := f.s.GormDB().Raw(`SELECT id FROM loop ORDER BY rowid DESC LIMIT 1`).Row().Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}
