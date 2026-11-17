package main

// S62 T1 · BK characterization goldens at the CLI level.
//
// These pin today's printed behaviour on the untouched code so the slice can
// prove what it changed and what it did not. Each header names the later task
// expected to change it and how:
//
//   - `db info` — T9 adds exactly ONE new final line (the backup line:
//     `backup   <age or never> · <dir>`); every line above it stands.
//   - `doctor` — T9 adds the BK15 lines (the database's own facts and the
//     backups' state, with the stale and pre-migration-divergence variants)
//     and fixes the socket leg's version floor (F-T1-1 below), so the
//     DAEMON-UP golden's first line changes from the refusal to reachable.
//   - `db upgrade --dry-run` — T5 adds NOTHING to stdout when no backup is
//     needed; when one is, the line is listed in T5.
//   - the fingerprint — T3's migration (the `backup_run` table) adds lines and
//     T3 updates this golden; nothing else in the slice touches it.
//
// Normalisation is the documented replacer in bkNormalise: the temp state
// path becomes $STATE, the byte sizes become $SIZE, and the doctor's
// backups-line age becomes $AGE — each is a measurement of a throwaway
// fixture, not behaviour. Nothing else varies, and anything that does
// after this commit is a bug, not noise.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

const bkStateToken = "$STATE"
const bkSizeToken = "$SIZE"
const bkAgeToken = "$AGE"

// bkDatabaseLine is the doctor's database line in the goldens' world:
// the fixture's fully migrated store in WAL mode, its sizes the
// throwaway file's measurements (bkSizeToken). The 79 is literal, like
// the fingerprint's migration count: a new migration file forces the
// regeneration, and the forced edit is the conscious update.
// 2026-11-03 (S48 T10 merge reconciliation): the count moved 80 -> 81
// when S59's environment migration landed on main, and the seed line
// below carries the registry's own size (main's 72 + S57 T9's nine gate
// names + S48 T10's five blueprint names = 88). Both are counts THIS
// file pins to a tree it must re-measure; the drift is the finding, not
// the fault.
// The same migration also drifted the two golden FILES (dbinfo, fingerprint);
// S57 T10's sweep regenerated them mechanically — the diff is S59's tables +
// the schema line 80->81 and nothing else.
// 2026-11-03 (S58 T1): the count moves 81 -> 82 with the supervision
// migration (six tables), the goldens regenerated the same mechanical way —
// S49 T1 (merged same day): automation_management.sql is the 38th file, the
// total 83; dbinfo's latest name and the count line moved with it, and the
// fingerprint picked up S49's tables beside S58's six.
// the diff is the six S58 tables + the count line; the golden's seed line
// also carries S59 T5's `tools.core up to date (2/2 applied)` it had been
// asserting stale against (a pre-existing red on main at 25a304b0, pinned in
// the S58 T0 baseline record — regeneration reconciles it, not S58's doing).
// 2026-11-03 (S59 T7): 83 -> 84 applied, the count both doctor goldens and the
// db-info goldens carry — any slice's migration moves it, and the edit is the
// conscious update the headers above each golden require.
// 2026-11-03 (S49 T6): 84 -> 85 applied — the automation_disable kind's migration
// (20261103193000), and 85 -> 86 with S59 T10's provision_owner_network (20261103203000), merged the same hour; any slice's migration moves the count, the edit is the
// conscious update the headers above each golden require. The two golden FILES
// (dbinfo, fingerprint) are regenerated the mechanical way in the same commit.
// 2026-11-04 (S50 T1/T5 + S60 T2, composed): 86 -> 87 with roadmap_change
// (20261104012856), 87 -> 89 with the merge queue's two files (20261104044821,
// 20261104044822), 89 -> 90 with proposal_roadmap_idea (20261104101500) —
// three same-day files from three worktrees, ordered by their timestamps;
// dbinfo's latest name and count line moved with the last of them.
// 2026-11-04 (S51 T1, unpaid here): 90 -> 91 with bug_change (20261104115500)
// — S51 paid the dbinfo/fingerprint goldens and the two bk window literals
// but left this constant, so the doctor goldens were main's red behind (the
// same unpaid-drift shape as the pending window above); this merge pays it.
// 2026-11-04 (S60 T9): 93 -> 94 with proposal_kind_mergequeue
// (20261104193000), the seventeenth kind on the proposal rebuild.
// 2026-11-04 (S52 T4): 92 -> 93 with proposal_learning_retract
// (20261104171000), the sixteenth kind on the proposal rebuild.
// 2026-11-04 (S52 T1): 91 -> 92 with learning_management (20261104132302),
// the house payment every migration file makes to every count pinned here.
// 2026-11-04 (S54 T1): 92 -> 93 with platform_change (20261104135327), the
// same house payment; dbinfo and fingerprint regenerated the mechanical
// way in the same commit — the diffs are the one new table (eight columns,
// five checks), its one index, the count lines, nothing else.
// 2026-11-04 (S54 T1 merge with origin/main): the two sides each moved
// 92 -> 93 (platform_change …135327 and proposal_learning_retract …171000,
// two same-day worktrees claiming the same 93); that merge truth was 94,
// paid in-merge with dbinfo regenerated over the merged tree.
// 2026-11-04 (S60 T9 landing merge, paid IN THE MERGE): 94 -> 95 with
// proposal_kind_mergequeue (20261104193000), the seventeenth kind on the
// proposal rebuild — NO new table, the CHECK list grows, so the golden
// diffs are the count line and the proposal kinds CHECK, regenerated the
// mechanical way in this commit. 2026-11-04 (S63 T1): 95 -> 96 with
// retro_facts (20261104215735) — four new tables, three triggers, four
// indexes; the fingerprint golden gains them, the dbinfo golden gains the
// count and the latest-name, regenerated the mechanical way in this commit.
// 2026-11-05 (S61 T1): 96 -> 97 with goal_intake (20261105024130) — six new
// tables, nine triggers, two indexes, and the goal.source_kind /
// roadmap_item.provenance CHECK lists each one word wider ('intake', Finding
// F-2 / P-C); the fingerprint golden gains them, the dbinfo golden gains the
// count and the latest-name, regenerated the mechanical way in this commit.
// 2026-11-05 (S27 T1): the constant itself held while the count held.
// 2026-11-05 (S27 T2, paid IN THE MERGE): the scheduler pair
// (20261105025313_scheduler, 20261105025314_loop_run_scheduler — five new
// tables, six triggers, four indexes, one loop_run rebuild with its widened
// CHECK) lands between goal_intake's …024130 and S61 T3's …093000, so the
// merged walk counts it 97 -> 99. 2026-11-05 (S61 T3):
// template_revision_system_actor (20261105093000) is 100 — one CHECK
// widening on scaffold_template_revision; dbinfo and fingerprint
// regenerate the mechanical way over the merged tree, and the diffs are
// those shapes plus the count and latest-name lines, nothing else.
// The same merge pays S63 T6's unpaid census drift: the three scorecard
// agent reads project into tools.core, 121 -> 124 behind, measured on
// this merged tree (main's golden still carried 121).
// 2026-11-05 (S61 T5): 100 -> 101 with proposal_kinds_intake (20261105110000) —
// the eighteenth and nineteenth kinds on the proposal rebuild; no new table,
// the CHECK list grows, so the fingerprint golden's only change is that one
// CHECK line beside the count and latest-name lines, regenerated the
// mechanical way in this commit.
// 2026-11-06 (S73 T3): 101 -> 102 with agent_change (20261106181435) — one
// NEW table (the agent plane's append-only ledger, AR1), no rebuild and no
// CHECK widening on anything existing. Measured at regeneration: the
// fingerprint golden grows by the table's seven columns, its six CHECKs and
// its one index — and NOT by its two triggers, because `db info
// --fingerprint` prints tables, columns, CHECKs and indexes only (measured:
// zero trigger lines in the 2159-line golden, so the mcp_change pair is not
// in it either); the append-only pair is pinned where it lives, by
// internal/store's TestAgentChangeRefusesAnUpdateAndADeleteByTrigger. The dbinfo
// golden moves its count line, its latest-name, and the whole table census —
// the census is six names a row, so one new table reflows every row after it
// (the layout, not a schema change; agent_change is the only NEW name in the
// list). The latest name is computed from the list (line ~229), only the
// count is literal, and both files were regenerated the mechanical way here.
const bkDatabaseLine = "database   " + bkStateToken + "/aeon/state.db · " + bkSizeToken + " · journal wal · 118 applied / 0 pending"

// bkNormalise applies the documented replacer: the throwaway state directory
// is substituted with $STATE, the size(s) on the `db info` WAL line and the
// doctor's database line are substituted with $SIZE, and the age on the
// doctor's backups line is substituted with $AGE. The size pattern accepts
// the plain shape (`4 KiB`), the split shape on the `db info` line (`4 KiB +
// 3.3 MiB in the -wal`), and the doctor's split shape (`4 KiB + 0 bytes
// WAL`), in bytes or in binary multiples. The age is a measurement of when
// the daemon's boot-time run happened relative to the doctor, so it is a
// token, like the sizes: the line's shape and its other words are pinned,
// the number is not.
func bkNormalise(out, stateDir string) string {
	out = strings.ReplaceAll(out, stateDir, bkStateToken)
	size := regexp.MustCompile(`\d+(\.\d+)? (?:KiB|MiB|GiB|bytes)( \+ \d+(\.\d+)? (?:KiB|MiB|GiB|bytes) (?:in the -wal|WAL))?`)
	// A function, not a literal: $ in a replacement literal is a group
	// reference, and the token would expand to nothing.
	out = size.ReplaceAllStringFunc(out, func(string) string { return bkSizeToken })
	// The age on the doctor's backups line: the coarsest-unit shape AgeHuman
	// produces (42s, 3m5s, 3h2m, 3d4h), anchored to the line so no other
	// number on another line can be mistaken for it. The line is matched
	// whole and the age word rebuilt in its place: a number-capturing
	// pattern would put the token in the replacement, and $AGE there would
	// be parsed as a group reference, not a literal.
	ageLine := regexp.MustCompile(`(?m)^backups    last \d+[smhd](?:\d+[smhd])? ago.*$`)
	return ageLine.ReplaceAllStringFunc(out, func(line string) string {
		numStart := len("backups    last ")
		numEnd := numStart
		for numEnd < len(line) {
			c := line[numEnd]
			if (c >= '0' && c <= '9') || c == 's' || c == 'm' || c == 'h' || c == 'd' {
				numEnd++
				continue
			}
			break
		}
		return line[:numStart] + bkAgeToken + line[numEnd:]
	})
}

// bkRun is aeonCommand with the streams separated: the goldens are contracts
// on STDOUT, and a merged stream would let a logrus line break the byte match.
func bkRun(t *testing.T, stateDir string, args ...string) (string, string, error) {
	t.Helper()
	cmd := aeonCommand(t, stateDir, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

// bkRunOnFixture is bkRun over a daemon fixture's environment, so the daemon
// is UP and the state dir is the fixture's.
func bkRunOnFixture(t *testing.T, f *daemonFixture, args ...string) (string, string, error) {
	t.Helper()
	cmd := exec.Command(aeonBinary, args...)
	cmd.Env = f.env()
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

func diffGolden(t *testing.T, name, got, want string) {
	t.Helper()
	if got == want {
		return
	}
	t.Fatalf("%s: output drifted from the golden.\n"+
		"  If the drift is a task this file's headers say is expected, update the golden AND say what changed.\n"+
		"  If it is not, the behaviour changed without a task asking for it — fix the code, not the golden.\n"+
		"--- got ---\n%s\n--- want ---\n%s", name, got, want)
}

// TestDBInfoGolden pins `aeon db info` with the daemon DOWN on a fresh
// directory, against testdata/bk/dbinfo.golden.
//
// T9 changes this: it appends exactly one final line (the backup line); the
// golden gains that line in the same commit, and the diff is the proof that
// it is the only change. The migration count and latest name are literal in
// the golden, like the fingerprint's: a new migration file from any slice
// forces the regeneration, and the forced edit is the conscious update (the
// same contract testdata/bk/fingerprint.golden already carries).
func TestDBInfoGolden(t *testing.T) {
	stateDir := t.TempDir()
	got, stderr, err := bkRun(t, stateDir, "db", "info")
	if err != nil {
		t.Fatalf("db info: %v\nstderr: %s", err, stderr)
	}
	want, err := os.ReadFile(filepath.Join("testdata", "bk", "dbinfo.golden"))
	if err != nil {
		t.Fatalf("read the golden: %v", err)
	}
	// The count line is asserted against the migration set separately, so a
	// regenerated golden that copies a stale count is caught even when the
	// table below it has not changed.
	all := store.Migrations()
	if !strings.Contains(string(want), fmt.Sprintf("schema  %d migration(s) applied, latest %s", len(all), all[len(all)-1].Name)) {
		t.Fatalf("the golden's schema line does not match the current migration set (len=%d); regenerate testdata/bk/dbinfo.golden", len(all))
	}
	diffGolden(t, "db info", bkNormalise(got, stateDir), string(want))
}

// TestDBInfoVectorsGolden pins `db info --vectors`.
//
// Byte-exact, no replacer: there is no path and no size in it. The version is
// the vendored extension's (mattn/go-sqlite3's linked sqlite-vec), so this
// golden changes only when the driver's version changes — and then it should.
func TestDBInfoVectorsGolden(t *testing.T) {
	stateDir := t.TempDir()
	got, stderr, err := bkRun(t, stateDir, "db", "info", "--vectors")
	if err != nil {
		t.Fatalf("db info --vectors: %v\nstderr: %s", err, stderr)
	}
	want := `sqlite-vec  v0.1.6  (statically linked)

There is no extension to install and no way to be missing it: it is compiled
into the binary (B3). A failure here means the binary was built without cgo.
`
	diffGolden(t, "db info --vectors", got, want)
}

// TestDBInfoFingerprintGolden pins `db info --fingerprint` against
// testdata/bk/fingerprint.golden (1294 lines: every table and column, every
// index).
//
// This is the schema's shape as a file, and it is the reference T2's snapshot
// verification and T4's export both compare against. T3's migration
// (the backup_run table, and its index if it gets one) adds lines: T3 updates
// the file in the same commit, and the diff is the reviewable proof of what
// the migration did to the schema.
func TestDBInfoFingerprintGolden(t *testing.T) {
	stateDir := t.TempDir()
	got, stderr, err := bkRun(t, stateDir, "db", "info", "--fingerprint")
	if err != nil {
		t.Fatalf("db info --fingerprint: %v\nstderr: %s", err, stderr)
	}
	want, err := os.ReadFile(filepath.Join("testdata", "bk", "fingerprint.golden"))
	if err != nil {
		t.Fatalf("read the golden: %v", err)
	}
	diffGolden(t, "db info --fingerprint", got, string(want))
}

// TestDBUpgradeDryRunGolden pins `db upgrade --dry-run`.
//
// Two shapes are reachable from a CLI: a fresh directory and a current store.
// A store seeded at an older vintage prints BYTE-IDENTICAL output to a fresh
// one, because opening the store applies the pending migrations FIRST (DB8)
// and the dry-run then reports what is left — nothing. The "was pending a
// moment ago" text is the parenthetical, and it is what F19 pins. The
// behind-vintage migration itself (and the file it leaves, or does not) is
// pinned at the store level by TestOpenMigratesAnOldStoreOnlyAfterACopy.
//
// T5 changes this only in one case: when the dry-run's open would migrate a
// store, T5's pre-migration backup runs, and T5's spec lists the line it adds.
// When no migration is pending, stdout is unchanged.
func TestDBUpgradeDryRunGolden(t *testing.T) {
	want := `no pending migrations — ` + bkStateToken + `/aeon/state.db is up to date
  (opening the store applies them, so this is the state AFTER that)
`
	for name, dir := range map[string]string{
		"fresh":   t.TempDir(),
		"current": t.TempDir(),
	} {
		// The "current" case gets a store first, the way a second CLI verb
		// would find one; the "fresh" case must not.
		if name == "current" {
			if _, stderr, err := bkRun(t, dir, "db", "info"); err != nil {
				t.Fatalf("seed the current store: %v\nstderr: %s", err, stderr)
			}
		}
		got, stderr, err := bkRun(t, dir, "db", "upgrade", "--dry-run")
		if err != nil {
			t.Fatalf("%s: db upgrade --dry-run: %v\nstderr: %s", name, err, stderr)
		}
		diffGolden(t, "db upgrade --dry-run ("+name+")", bkNormalise(got, dir), want)
	}
}

// TestDBQueryAwaitingGolden pins `db query awaiting` against a RUNNING daemon.
//
// The daemon is up for this one: the verb dials the endpoint (like every
// non-direct verb), and "nobody home" is a different sentence. Nothing in
// S62 changes this output.
func TestDBQueryAwaitingGolden(t *testing.T) {
	f := startDaemon(t)
	got, stderr, err := bkRunOnFixture(t, f, "db", "query", "awaiting")
	if err != nil {
		t.Fatalf("db query awaiting: %v\nstderr: %s", err, stderr)
	}
	want := `SELECT 1 FROM question
WHERE component_root = ? AND loop_slug = ? AND answered_at IS NULL LIMIT 1
→ no loops awaiting an answer

It is DERIVED, not stored: a flag and an unanswered question can disagree, and
when they do nothing says which is right (DB6).
`
	diffGolden(t, "db query awaiting", got, want)
}

// TestDoctorGoldenDown pins `aeon doctor` with the daemon DOWN on a fresh
// directory.
//
// T9 changes this: it appends the four BK15 lines (backup directory, last
// backup, retention, pre-migration state). Everything above them stands.
//
// The seed lines are content-free counts against the seed declarations as of
// this commit: a slice that adds or changes a seed set changes them, and the
// golden edit is the conscious update that requires.
//
// S62 T6 (2026-11-02): tools.core 70 -> 71 — backup_status, the backup
// family's one read twin, joins the registry the seed status counts.
// Merge 2026-11-02 (main gained S47 T7/T8 before this task landed): 71 -> 73,
// measured on the merged tree — S47 T7's two own-view tool reads (tools_mine,
// tool_show) join the set the seed status counts (the set runs the registry
// plus its two unlisted entries, so the registry's 70 -> 71 reads 72 -> 73 here).
// S62 T8 (2026-11-02): 73 -> 74 — export_sets, the export family's one read
// twin, joins the registry the seed status counts.
// S51 T5 (2026-11-04, row written at T6's sweep): 106 -> 109 — bug_list,
// bug_show and bug_file, the bug family's two reads and its ONE filing
// twin (TM6), join the registry the seed status counts. T5's landing
// moved them; this golden's drift record is where that is spoken.
// S59 T7 (2026-11-03): +5 — the execution-environment
// family's five agent tools (four reads + the one propose ask) join the registry the
// seed status counts (93 on that tree; the merge with S58 T6 lands 98 below); bkDatabaseLine's applied count moves 83 -> 84 with the 13th
// proposal kind's migration (20261103181500_proposal_environment_restrict).
// S48 T4 (2026-11-02): the blueprints line joins — the database is
// authoritative; a fresh untouched store holds 0 rows (the daemon's arrival
// seed and the adoption run at serve), the up fixture has seeded 11.
// S58 T6 (2026-11-03): the tail grows THREE lines and the seed count moves
// 88 -> 98 (+5 supervision reads; +5 S59 T7's environment names landed the
// same day — merge reconciliation, measured on this merged tree) — WHAT CHANGED, in the open: the notifications leg (daemon held:
// the "start it with aeon serve" line, never silence), the background
// capability line (T7's, honest NOT REPORTED until the tray exists), and
// five supervision read tools joining the tools.core seed set the seed
// status counts. The Up golden grows the daemon-held pair (0 channels + the
// SV1 source census) instead of the held-less line.
// S49 T7 (2026-11-04): 98 -> 103 — the automation family's five agent tools
// (four reads + the one propose-to-disable ask) join the registry the seed
// status counts: 101 names + the two unlisted entries, measured on this
// merged tree; the merge's other golden moves (bkDatabaseLine 87, pending 27)
// are S50 T1's roadmap_change drift, paid in the same commit.
// S60 T7/T8 (2026-11-04): the blueprints.landing seed line joins both goldens and
// the blueprint count moves 12 -> 14 — the landing+resolution blueprints SHIP
// (landingseed, mounted through the seed engine); both moves mechanical, the
// census line and the count are the seed set's, not a behaviour change.
// S60 T10 (2026-11-04): 113 -> 120 — the same ledger's line was left stale
// by the census that grew it (measured RED on the clean pushed HEAD and on
// origin/main's own copy of this file, which still read 113: inherited,
// never introduced here; T10 registers zero tools — TestActionCheckGolden
// and the mergequeue verb census pin that). The +7 are registry entries,
// S60 T9's five agent mergequeue tools among them (its Notes' five-name
// allowlist) and the rest other slices' agent tools merged to main; each
// owner paid its own want-list guard, this golden waited for a change to
// land beside it. This is that change: the header's "update the golden AND
// say what changed", and what changed is a count of seed rows.

// Re-bumped 113 -> 120 -> 121, 2026-11-04 (S60 T12's close-out run): S54 T4's
// index_status tool (58937445) joined main beside S60's merge work and grew
// the registry again; its owner paid the class-list and catalog goldens, this
// doctor line waited for any change to land beside it — T11's close ran the
// UI and targeted Go subsets and did NOT re-run the cmd goldens, a gap T12
// names here and pays now. The same law: count of seed rows, nothing else.
// S50 T2 (2026-11-04): blueprints.standard 11 -> 12 — the roadmap-plan
// record joins the standard set (RM3), so the down census counts twelve
// behind and the up census carries the same set; the tools.core line is
// untouched (roadmap's agent tools join at T5), the database line is
// unchanged (T2 adds no migration).
// S51 T5 (2026-11-04): 106 -> 109 — the bug family's three (TM6: bug_list,
// bug_show, and the ONE filing twin bug_file); not in DefaultGrant().
// The DOWN golden's census line was missed in that same pass (main was red
// on it); S52 T3 paid it on 2026-11-04 — 109 behind, measured.
// S52 T4 paid it again the same day — 113 behind, measured: the memory
// four (learning_query, learning_show, learning_add,
// learning_propose_retract) project in with their catalog specs.
// S50 T5 (2026-11-04): 103 -> 106 — the roadmap family's three agent tools
// (two reads + the one propose-to-add-idea ask) join the registry the seed
// status counts: 104 names + the two unlisted entries, measured on this
// merged tree; the database line moves 87 -> 88 with proposal_roadmap_idea.
// S54 T5 (2026-11-04): 120 -> 121 — S54 T4's index_status catalog spec
// projects into the tools.core seed set. Named honestly: T4 CREATED the
// drift and missed it (its cmd/aeon validation ran targeted patterns that
// did not include Doctor); the first FULL cmd/aeon run, on T5's tree,
// caught the red and it reproduced verbatim on pristine origin/main —
// paid here, one line, measured on this merged tree.
// 2026-11-05 (S27 T2): both census blocks gain the seed retro.templates
// line S63 T1's retro templates set projects (down: 5 behind; up: the
// running daemon's engine applies it, 1/1) — the drift was a pre-existing
// main red, unpaid when S63 T1 landed; the database line moves 96 -> 98
// with S27 T2's scheduler pair.
func TestDoctorGoldenDown(t *testing.T) {
	stateDir := t.TempDir()
	got, stderr, err := bkRun(t, stateDir, "doctor")
	if err != nil {
		t.Fatalf("doctor: %v\nstderr: %s", err, stderr)
	}
	// 2026-11-05 (S61 T1 merge, paid IN THE MERGE): S63 T1's
	// retro.templates seed set (5 templates) moved this census; the UP
	// golden rode with it and the DOWN golden did not — the drift the S61
	// baseline named. Pay-forward per S60 T12's precedent; the added line
	// is the run's own words.
	// 2026-11-05 (S61 T3): the templates.scaffold seed set (3 shipped
	// starters, GI14) joins this census — the set is inert and untouched on
	// a daemon-down store, so it reports behind, three records.
	// 2026-11-05 (S61 T9): the intake plane's two inert sets join this
	// census — the interview's voice (agents.intake, 1 record) and the
	// goal-intake blueprint (blueprints.intake, 1 record); a daemon-down
	// store touches neither, so both report behind. Paid forward with the
	// same run's other drift: tools.core 124 → 137 — main's tools landed
	// since this golden was last paid (S27 T7's twelve scheduler tools
	// among them), the run's own words, S60 T12's precedent.
	// 2026-11-08 (documents): the UP golden reads agents.firstParty 1/1 -> 2/2 — the
	// set version bumped so the rewritten bodies reach a live store (agentseed/content.go).
	// 2026-11-09 (agent tech): the UP golden reads agents.firstParty 2/2 -> 3/3 — the
	// set version bumped so the tech tags reach a live store (agentseed/content.go).
	// 2026-11-10 (blueprint override): the database line 108 -> 109 applied (blueprint_override).
	// 2026-11-15 (loop_access, and the permissions.firstParty default set + the inbox's gates/proposals/triage
	// sources): 109 -> 110 applied (loop_access, 20261115120000), one more seed line, and the notify census.
	// 2026-11-09 (MCP title): the database line 107 -> 108 applied (mcp_title).
	// 2026-11-08 (MCP share): the database line 106 -> 107 applied (mcp_share).
	// 2026-11-08 (MCP env binding): the database line 105 -> 106 applied (mcp_env_from).
	// 2026-11-08 (variables): tools.core 141 -> 143 (env_list, env_get) and the database
	// line 104 -> 105 applied (env_var).
	// 2026-11-08 (documents): tools.core 137 -> 141 — the deliverables door's
	// four (doc_write, doc_edit, doc_read, doc_list) join the registry; the
	// database line rides 102 -> 104 applied (loop_todo, unpaid by its own
	// landing, and document_owner).
	want := `daemon socket    unreachable: no daemon at ` + bkStateToken + `/nothing.sock: dial unix ` + bkStateToken + `/nothing.sock: connect: no such file or directory
  start one with ` + "`aeon serve`" + `
narrow surface   not listening — nothing binds without an explicit address, and an address without a key refuses to start (that is the design, not a limitation)
seed agents.firstParty 33 behind (aeon seed status)
seed agents.intake 1 behind (aeon seed status)
seed blueprints.intake 1 behind (aeon seed status)
seed blueprints.landing 2 behind (aeon seed status)
seed blueprints.standard 12 behind (aeon seed status)
seed environments.standard 1 behind (aeon seed status)
seed mcp.grants up to date (0/1 applied)
seed mcp.servers up to date (0/1 applied)
seed notify.events 14 behind (aeon seed status)
seed permissions.firstParty 340 behind (aeon seed status)
seed policy.corpus 367 behind (aeon seed status)
seed retro.templates 5 behind (aeon seed status)
seed templates.scaffold 3 behind (aeon seed status)
seed tools.core 144 behind (aeon seed status)
seed verify.templates 13 behind (aeon seed status)
seed watchdog.default 5 behind (aeon seed status)
mcp           no MCP configuration (level-0)
blueprints    database authoritative (0 blueprints)
` + bkDatabaseLine + `
backups    never backed up — run ` + "`aeon backup run`" + `
` +
		"notifications    the daemon holds channels, secrets and the source census — start it with " + "`aeon serve`" +
		"\nbackground        not reported — no tray or background agent has told this daemon it exists (S58 T7)\n"
	// The database line names a store that EXISTS in a "fresh" doctor
	// run: the seed leg above it opened and migrated it (the seed
	// family's direct door, S62 T5's pre-migration safety net) before
	// T9's backup leg read the file. The line reports the file as it
	// is at the moment the doctor finishes — 79 applied, 0 pending,
	// WAL mode. T9's own leg (database + backups) opens nothing:
	// TestDoctorDoesNotOpenTheStore pins the new path, and the
	// store-not-created sentence is pinned where no leg can create the
	// file (internal/doctor's TestBackupFactsNeverTouchTheStore).
	diffGolden(t, "doctor (down)", bkNormalise(got, stateDir), want)
}

// TestDoctorGoldenUp pins `aeon doctor` with the daemon UP.
//
// F-T1-1, FOUND BY T1'S CAPTURE AND PINNED ON PURPOSE, FIXED BY T9: the
// socket leg once printed "unreachable: aeon -31002: protocol version not
// supported" — a TRUE line about a LIVE daemon. The old leg dialed with the
// literal `1`, and since S38 T2 the protocol floor is 2, so a version-1
// client was refused before the handshake. T9 (2026-11-03) dials with the
// floor at 0 like every other verb, and this golden carries the fix as its
// reviewed change: the first line now says reachable.
//
// The seed lines pin the SETTLED state, after the daemon's boot seeding has
// finished: agents.firstParty, tools.core and watchdog.default are up to
// date (1/1 — the full sets applied at start), mcp.grants and mcp.servers
// are up to date with an empty set (0/1), policy.corpus is never
// auto-seeded so it stands 367 behind, and S58 T4 adds notify.events —
// the shipped event-class table, inert, applied at boot like the rest.
//
// The settle wait below is what makes that pin deterministic. The socket
// answers before the boot seeding runs (localrpc binds at Init, not Serve,
// and ApplyInertAtStart is synchronous in the recovery path — after the
// socket is already reachable), so a doctor that races it sees mid-seed
// counts. T1's golden caught one such race and read it as "the 4 the daemon
// seeds at boot"; boot seeding has since come to apply the full firstParty
// set at start, and the settled state is what this pin now carries.
// S58 T6 (2026-11-03): the tail grows the daemon-held notify pair —
// `notifications    0 channels (a channel is born disabled; notify channel
// create makes one)` and the SV1 source census `notify sources
// questions:reporting ... budget:not-wired ...` — plus the background
// capability line, and tools.core counts 98 with the five supervision
// read twins and S59 T7's five environment names (88 + 5 + 5, measured). The census names every unwired source as `not-wired`: on a
// daemon with no budget/stuck/gates/scheduler wiring this line is the SV1
// truth, and a future wire RE-OLDS this golden on purpose.
func TestDoctorGoldenUp(t *testing.T) {
	f := startDaemon(t)
	bkWaitForBootSettles(t, f)
	got, stderr, err := bkRunOnFixture(t, f, "doctor")
	if err != nil {
		t.Fatalf("doctor: %v\nstderr: %s", err, stderr)
	}
	// S62 T9 (2026-11-03): the first line is the F-T1-1 fix in this run —
	// the socket leg dials with the protocol-version floor at 0
	// ("whatever you support", the same dial every other verb makes), so
	// the doctor now reports its own daemon reachable, and the four BK15
	// lines stand under the mcp line: the database's own facts (the store
	// exists in the fixture, fully migrated, WAL mode) and the backups'
	// answer from the DAEMON'S join — one file, the daemon's own
	// boot-time run (a fresh machine is due at boot), unverified because
	// the ledger's verify rows are written only by an explicit
	// `backup verify` or a post-restore check, and the run's own
	// VerifyAfter is the snapshot's level-2 check at publish, not a
	// ledger verdict. The age is $AGE (a measurement, like the sizes):
	// the wait above makes the run a fact, not the number a fact.
	// 2026-11-05 (S61 T3): templates.scaffold joins the UP census too — the
	// fixture's daemon applies the inert set at boot, so it reports up to
	// date (1/1 applied), like every other boot-applied set.
	// 2026-11-05 (S61 T9): agents.intake and blueprints.intake join the UP
	// census the same way — the interview's voice and its blueprint are
	// applied at boot; and the blueprint census moves 14 → 15 with the
	// goal-intake row the daemon applies.
	want := `daemon socket    reachable — the primary surface (0600 path, peer credentials, P26)
narrow surface   not listening — nothing binds without an explicit address, and an address without a key refuses to start (that is the design, not a limitation)
seed agents.firstParty up to date (3/3 applied)
seed agents.intake up to date (1/1 applied)
seed blueprints.intake up to date (1/1 applied)
seed blueprints.landing up to date (1/1 applied)
seed blueprints.standard up to date (2/2 applied)
seed environments.standard up to date (1/1 applied)
seed mcp.grants up to date (0/1 applied)
seed mcp.servers up to date (0/1 applied)
seed notify.events up to date (1/1 applied)
seed permissions.firstParty up to date (1/1 applied)
seed policy.corpus 367 behind (aeon seed status)
seed retro.templates up to date (1/1 applied)
seed templates.scaffold up to date (1/1 applied)
seed tools.core up to date (2/2 applied)
seed verify.templates up to date (1/1 applied)
seed watchdog.default up to date (1/1 applied)
mcp           no MCP configuration (level-0)
blueprints    database authoritative (15 blueprints)
` + bkDatabaseLine + `
backups    last ` + bkAgeToken + ` ago (unverified) · 1 kept · ` + bkStateToken + `/aeon/backups` +
		"\nnotifications    0 channels (a channel is born disabled; `notify channel create` makes one)" +
		"\nnotify sources    questions:reporting  blocked:reporting  stopped:reporting  gates:reporting  proposals:reporting  triage:reporting  budget:not-wired  stuck:not-wired  scheduler:not-wired" +
		"\nbackground        not reported — no tray or background agent has told this daemon it exists (S58 T7)\n"
	diffGolden(t, "doctor (up)", bkNormalise(got, f.stateDir), want)
}

// bkWaitForBootSettles polls the daemon until its boot seeding has finished
// AND its boot-time backup run has completed — the two things the
// TestDoctorGoldenUp pin depends on:
//
//   - the seed lines pin the SETTLED seed state, so the wait polls the
//     agents.firstParty set until it lists no unapplied records (the S43 T6
//     ApplyInertAtStart seeding is done);
//   - the backups line pins the daemon's own first scheduled run (a fresh
//     machine has never backed up, so the schedule is due at boot, and the
//     daemon runs one before the doctor asks), so the wait also polls
//     `backup status --json` until the state is fresh and nothing is
//     running.
//
// The socket answers before either settles (localrpc binds at Init, not
// Serve), so a doctor that races them sees mid-seed counts or a mid-run
// backups line; both waits turn the pin from a race into a fact.
func bkWaitForBootSettles(t *testing.T, f *daemonFixture) {
	t.Helper()
	deadline := time.Now().Add(90 * time.Second)

	// The seed half: settled when the set lists no unapplied records.
	for {
		out, err := f.run("seed", "status", "--set", "agents.firstParty")
		if err == nil && !strings.Contains(out, "\n  - ") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the daemon's boot seeding did not settle within the deadline:\n%s", out)
		}
		time.Sleep(250 * time.Millisecond)
	}

	// The backup half: settled when the boot run has a ledger row and the
	// lock is free. The state machine's answer is the daemon's own —
	// fresh means a success exists, not-running means the file stands.
	for {
		out, err := f.run("backup", "status", "--json")
		if err == nil {
			var st protocol.BackupStatusView
			if json.Unmarshal([]byte(out), &st) == nil && st.State != "never" && !st.Running {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("the daemon's boot backup did not settle within the deadline: %s", out)
		}
		time.Sleep(250 * time.Millisecond)
	}
}
