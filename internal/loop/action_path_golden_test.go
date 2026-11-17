package loop

// action_path_golden_test.go — S57 T1 (characterization, P2 Phase A). The
// engine's CODE-ACTION path, pinned on the untouched tree (1ae086c0) by
// driving the real engine (`Start` → `Run` → `Pinned`) with a fake action
// service:
//
//   - success: the action runs, its evidence lands under the STEP KEY, the
//     step state flips to done, `loop.action.executed` fires with the
//     pinned payload, and the position ADVANCES (a completed code action
//     moves; it does not spin).
//   - failure: dispatch error → blocked `action-failed` with the error
//     VERBATIM as the reason; no `loop.action.executed`, no advance.
//   - deny: an irreversible action on an unattended loop blocks `action-
//     denied` with T2's sentence VERBATIM, BEFORE the forge is ever asked
//     (the fake's call log is empty).
//   - on_reject: the T0 item-1 answer, tightened by S57 T6 (VG7) — the
//     validator refuses a code action other than verify.run carrying
//     `on_reject` (the engine routes only the verify run's outcome; the
//     other actions' results are facts the loop records, not verdicts it
//     routes), accepts verify.run's edge, and the door (Start) speaks
//     the invalid-blueprint sentence for the refused shape. A code
//     action's FAILURE still blocks `action-failed` and never routes.
//   - actionArgs: the pure builder's output for a representative step —
//     Dir/Loop/Task/TaskTitle/PRSummary/Base are derived, StepID and
//     Attempt are ABSENT (zero) — the two fields T6's `aeon_review`-era
//     args must add on purpose.
//
// T6 (VG7) maps the unverified reason onto this path and must leave every
// sentence and payload byte-identical; T3's `verify.run` enters this same
// dispatch as another registry name. The `loopId` in event payloads is
// normalized to `<loopId>` (the store mints it from crypto/rand).
//
// Generation (run once, on the untouched tree):
//
//	AEON_WRITE_GOLDEN=1 go test ./internal/loop/ -run TestCodeActionPathGoldens -count=1 -tags sqlite_fts5

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/cjlapao/aeon/internal/action"
	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/forge"
	"github.com/cjlapao/aeon/internal/store"
)

const goldActionPath = "testdata/action_path_golden.json"

type goldActionEvent struct {
	Kind    string         `json:"kind"`
	Payload map[string]any `json:"payload"`
}

type goldActionRow struct {
	Case       string                       `json:"case"`
	Status     string                       `json:"status"`
	LastReason string                       `json:"lastReason"`
	Position   Position                     `json:"position"`
	LaneStates map[string]string            `json:"laneStates"`
	StepStates map[string]StepState         `json:"stepStates"`
	Evidence   map[string]map[string]string `json:"evidence"`
	Counters   map[string]int               `json:"counters"`
	Events     []goldActionEvent            `json:"events"`
	ForgeCalls []goldFakeCall               `json:"forgeCalls"`
	AuditCalls int                          `json:"auditCalls"`
	// No omitempty: the validator half pins the ABSENCE of diagnostics as
	// an empty list, and an omitted field would let a missing check read
	// as a passed one.
	Diagnostics []blueprint.Diagnostic `json:"diagnostics"`
	ArgsJSON    string                 `json:"argsJson,omitempty"`
}

// normalizeLoopID rewrites the store-minted loop id in event payloads so
// the golden is reproducible; every other byte of the payload is pinned.
func normalizeLoopID(payload map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range payload {
		if k == "loopId" {
			out[k] = "<loopId>"
			continue
		}
		out[k] = v
	}
	return out
}

// driveRow runs one engine case to its durable end and reads back what the
// pin, the event log and the fakes saw.
func goldActionDriveRow(t *testing.T, name string, lanes string, allowOutward bool, audit func() (int, error), with map[string]string) goldActionRow {
	t.Helper()
	s := openLoopStore(t)
	f := forge.NewFake()
	path := filepath.Join(t.TempDir(), "ap.yaml")
	writeBlueprint(t, path, lanes)
	st, err := Start(context.Background(), s, store.NewLoop{
		Slug: "gld-ap-" + name, ComponentRoot: goldRoot, Root: goldRoot, Mode: "standard",
		AllowOutward: allowOutward,
	}, path, boolPtr(false), with)
	if err != nil {
		t.Fatal(err)
	}
	auditCalls := 0
	svc := &action.Services{
		Forge: &action.ForgeActions{Forge: f, Git: goldBranchProbe{}},
		Audit: func(ctx context.Context) (int, error) {
			auditCalls++
			return audit()
		},
	}
	events := []goldActionEvent{}
	dep := Deps{
		Store:   s,
		Actions: func(string) *action.Services { return svc },
		Event: func(kind string, payload any) {
			if m, ok := payload.(map[string]any); ok {
				events = append(events, goldActionEvent{Kind: kind, Payload: normalizeLoopID(m)})
			} else {
				events = append(events, goldActionEvent{Kind: kind})
			}
		},
	}
	if err := Run(context.Background(), dep, st.ID); err != nil {
		t.Fatal(err)
	}
	saved, err := Pinned(context.Background(), s, st.ID)
	if err != nil {
		t.Fatal(err)
	}
	row := goldActionRow{
		Case:       name,
		Status:     saved.Status,
		LastReason: saved.LastReason,
		Position:   saved.Position,
		LaneStates: saved.LaneStates,
		StepStates: saved.StepStates,
		Evidence:   saved.Evidence,
		Counters:   saved.Counters,
		Events:     events,
		ForgeCalls: []goldFakeCall{},
		AuditCalls: auditCalls,
	}
	for _, c := range f.Calls() {
		row.ForgeCalls = append(row.ForgeCalls, goldFakeCall{Method: c.Method, Args: c.Args})
	}
	return row
}

func goldActionRows(t *testing.T) []goldActionRow {
	auditOK := func() (int, error) { return 42, nil }
	auditFail := func() (int, error) { return 0, errAuditUnreadable }
	rows := []goldActionRow{
		// Success: local action runs unattended, evidence under the step
		// key, the executed event, and the position advances off the lane.
		goldActionDriveRow(t, "audit-success-advances",
			"  - id: run\n    step: {kind: code-action, action: audit}\n",
			false, auditOK, nil),
		// Failure: the dispatch error becomes the block reason VERBATIM.
		goldActionDriveRow(t, "audit-failed-blocks",
			"  - id: run\n    step: {kind: code-action, action: audit}\n",
			false, auditFail, nil),
		// Deny: irreversible on an unattended loop refuses before dispatch.
		goldActionDriveRow(t, "merge-denied-unattended",
			"  - id: run\n    step: {kind: code-action, action: forge.merge}\n",
			false, auditOK, nil),
		// Representative outward run: unattended + the start-time
		// allowOutward declaration lifts openPR; the fake's log is the
		// witness of the args the engine built (Task→head, Base,
		// TaskTitle→title, PRSummary→body).
		goldActionDriveRow(t, "openpr-allowoutward-representative",
			"  - id: run\n    step: {kind: code-action, action: forge.openPR}\n",
			true, auditOK, taskIdentity()),
		// S57 T6 (VG7) moved this pin: a code action's FAILURE still
		// blocks `action-failed` and never routes — but a code action
		// other than verify.run can no longer CARRY the edge, so the
		// old shape (audit + on_reject) no longer starts. The engine's
		// routing half now lives on verify.run (TestVerifyRunLoopFail,
		// verify_loop_test.go) and the tightening's own pins are the
		// validator and door cases below.
	}

	// on_reject, validator half — S57 T6 (VG7) tightened the shape the
	// T0 item-1 recording named: a code action other than verify.run
	// carrying on_reject is now an Invalid diagnostic (the engine routes
	// only the verify run's outcome; the other actions' results are
	// facts the loop records, not verdicts it routes). The diagnostics
	// lists are the pins: one finding with its sentence for audit,
	// none for verify.run (the edge it carries still validates).
	{
		validateOnReject := func(name, lanes string) goldActionRow {
			path := filepath.Join(t.TempDir(), name+".yaml")
			writeBlueprint(t, path, lanes)
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			bp, err := blueprint.Load(raw)
			if err != nil {
				t.Fatalf("the validator's own test fixture must parse: %v", err)
			}
			report := blueprint.Validate(bp, blueprint.ValidationOptions{Mode: "standard", Actions: action.Names()})
			return goldActionRow{Case: name, Diagnostics: report.Diagnostics}
		}
		rows = append(rows,
			validateOnReject("on-reject-audit-refused",
				"  - id: build\n    iterate: tasks\n    ladder:\n"+
					"      - {id: s1, kind: code-action, action: audit, on_reject: {goto: s2, then: block}}\n"+
					"      - {id: s2, kind: code-action, action: audit}\n"),
			validateOnReject("on-reject-verify-run-accepted",
				"  - id: build\n    iterate: tasks\n    ladder:\n"+
					"      - {id: s1, kind: code-action, action: verify.run, on_reject: {goto: s2, then: block}}\n"+
					"      - {id: s2, kind: code-action, action: audit}\n"))
	}

	// on_reject, door half: the tightened shape is refused where a loop
	// is born — Start speaks the invalid-blueprint sentence and creates
	// nothing (the pin and the row are one transaction).
	{
		lanes := "  - id: run\n    step: {kind: code-action, action: audit, on_reject: {goto: finish, then: block}}\n" +
			"  - id: finish\n    step: {kind: code-action, action: audit}\n"
		path := filepath.Join(t.TempDir(), "ap-door.yaml")
		writeBlueprint(t, path, lanes)
		_, err := Start(context.Background(), openLoopStore(t), store.NewLoop{
			Slug: "gld-ap-onreject", ComponentRoot: goldRoot, Root: goldRoot, Mode: "standard",
		}, path, boolPtr(false), nil)
		var refusal PinRefusal
		if err == nil || !errors.As(err, &refusal) || refusal.Kind != "invalid-blueprint" {
			t.Fatalf("a code action other than verify.run carrying on_reject must be refused at the door, got %v", err)
		}
	}

	// actionArgs, the pure builder: the representative step's full Args —
	// the derived fields present, StepID/Attempt absent (zero) today.
	{
		st := &LoopState{
			Slug: "gld-args", Root: goldRoot,
			RuntimeState: RuntimeState{
				With:     taskIdentity(),
				Evidence: map[string]map[string]string{"run": {"pr_number": "7", "issue_number": "3"}},
			},
		}
		st.Position = Position{Lane: "run"}
		act := Action{Position: st.Position, Step: &blueprint.Step{Action: action.ForgeOpenPR}}
		a := actionArgs(st, act)
		b, err := json.Marshal(a)
		if err != nil {
			t.Fatal(err)
		}
		rows = append(rows, goldActionRow{Case: "actionargs-representative", ArgsJSON: string(b)})
	}
	return rows
}

// errAuditUnreadable is the scripted dispatch failure: its text is pinned
// VERBATIM in the golden as the block reason.
var errAuditUnreadable = errors.New("the audit table is unreadable: file lock")

func TestCodeActionPathGoldens(t *testing.T) {
	rows := goldActionRows(t)
	got, err := json.MarshalIndent(rows, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')

	if os.Getenv("AEON_WRITE_GOLDEN") == "1" {
		if err := os.MkdirAll(filepath.Dir(goldActionPath), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(goldActionPath, got, 0600); err != nil {
			t.Fatal(err)
		}
		t.Logf("wrote %s (%d rows)", goldActionPath, len(rows))
		return
	}
	want, err := os.ReadFile(goldActionPath)
	if err != nil {
		t.Fatalf("no golden: %v — run once with AEON_WRITE_GOLDEN=1 on the untouched tree (S57 T1)", err)
	}
	if string(got) != string(want) {
		t.Fatalf("the code-action path moved off its golden:\n--- golden ---\n%s--- current ---\n%sT6 (VG7) refactors this path and must leave every sentence and payload byte-identical; a move here is a deliberate, listed amendment in that task's Notes.", want, got)
	}
}
