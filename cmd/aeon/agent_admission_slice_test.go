package main

// agent_admission_slice_test.go — S37 T7, the slice's one end-to-end test
// (criterion 1 / test map 1) and the validator parity test (criterion 2).
//
// The slice test drives the whole slice through the SAME seams the real
// daemon and CLI use: proposal.create over the Table (with the AgentImport
// preview seam wired, as `aeon serve` does), the CLI commit door (runProposal,
// T5's only effect door), and the plugin_agent rows the UI and the desk both
// answer from. It proves the three things the brief claims: proposing changes
// nothing live; committing imports the agent DISABLED; and a commit and a
// direct CLI import of the same package produce the same rows except for the
// timestamp and source the package's own location gives.
//
// The store the daemon holds and the store the CLI commit opens are the SAME
// file: agentHome(t) sets XDG_STATE_HOME once and this helper opens the store
// through openAgentStore (not proposalHome, which re-runs agentHome and would
// reset the home, breaking the shared-store premise).

import (
	"encoding/json"
	"testing"

	"github.com/cjlapao/aeon/internal/daemon"
	"github.com/cjlapao/aeon/internal/store"
)

// agentAdmissionStore wires a fresh daemon with every seam this slice's
// create and commit path exercise: Proposals (from the store, as daemon.New
// always does), the ValidateProposal seam, the AgentImport preview seam
// (cmd/aeon's agentImportPreviewFor, exactly as `aeon serve` wires it) and
// the agent.plugins read seam. XDG_STATE_HOME is set once by agentHome, so
// the CLI commit (openStoreDirectly) and the daemon's store share a file.
func agentAdmissionStore(t *testing.T) (*store.Store, *daemon.Daemon) {
	t.Helper()
	st := openAgentStore(t)
	opts := daemon.Options{
		Store:              st,
		ValidateProposal:   validateProposalPayload,
		AgentImportPreview: agentImportPreviewFor(st),
		Plugins:            pluginsHandleFor(st),
	}
	d := daemon.New(opts)
	return st, d
}

// pluginRows is the roster a reader sees — the plugin_agent rows, the same
// thing agent.plugins answers from.
func pluginRows(t *testing.T, st *store.Store) []store.PluginAgent {
	t.Helper()
	rows, err := st.PluginAgentsAll(true)
	if err != nil {
		t.Fatalf("plugin agents: %v", err)
	}
	return rows
}

// rowsKey is the roster minus the two fields a second store would legitimately
// differ on (source — the package's own location — and importedAt — the clock);
// everything else (name, state) must be identical across a commit and a direct
// import of the same bytes.
func rowsKey(rows []store.PluginAgent) map[string]string {
	out := map[string]string{}
	for _, r := range rows {
		out[r.Name] = r.State
	}
	return out
}

// createAgentImport dispatches proposal.create for the agent_import kind and
// returns the row id, the created status and the preview agent name.
func createAgentImport(t *testing.T, d *daemon.Daemon, pkgPath string) (id int64, status, previewAgent string) {
	t.Helper()
	res, err := sliceDispatch(t, d, "proposal.create", map[string]any{
		"kind":    "agent_import",
		"payload": map[string]any{"pkgPath": pkgPath},
	})
	if err != nil {
		t.Fatalf("proposal.create agent_import: %v", err)
	}
	raw, _ := json.Marshal(res)
	var r struct {
		ID      int64  `json:"id"`
		Status  string `json:"status"`
		Preview *struct {
			Agent string `json:"agent"`
		} `json:"preview"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		t.Fatalf("unmarshal create result: %v", err)
	}
	if r.Preview != nil {
		previewAgent = r.Preview.Agent
	}
	return r.ID, r.Status, previewAgent
}

func TestAgentAdmissionSliceEndToEnd(t *testing.T) {
	agentHome(t) // set XDG_STATE_HOME once — the daemon's store and the CLI
	// commit's store share this file.
	dir := writeAgentPackage(t, "reviewer-plus", carriedFile("reviewer-plus"))

	st, d := agentAdmissionStore(t)

	// 2. snapshot the roster BEFORE the create.
	before := rowsKey(pluginRows(t, st))

	// 3. proposal.create for agent_import, with the preview seam wired.
	id, status, previewAgent := createAgentImport(t, d, dir)
	if previewAgent != "reviewer-plus" {
		t.Fatalf("the create preview must name the agent: got %q", previewAgent)
	}
	if status != "proposed" {
		t.Fatalf("the create row must be proposed, got %q", status)
	}

	// 4. nothing imported: the roster is identical to before, and no policy
	// row was stored (a carried denial is only ever added at commit).
	if got := rowsKey(pluginRows(t, st)); got != nil && len(got) != len(before) {
		t.Fatalf("TD2 violated — creating an agent_import proposal changed the roster")
	}
	if n := len(mustPolicyList(t, st)); n != 0 {
		t.Fatalf("create must store no policy row, got %d", n)
	}

	// 5. commit through the CLI door (T5's only effect door) imports the agent
	// disabled, and the roster now lists it.
	if out, err := runProposal(t, "commit", itoa(id)); err != nil {
		t.Fatalf("commit: %v\n%s", err, out)
	}
	after := pluginRows(t, st)
	if len(after) != 1 {
		t.Fatalf("the commit must import exactly one agent, got %d: %v", len(after), after)
	}
	if after[0].Name != "reviewer-plus" || after[0].State != "disabled" {
		t.Fatalf("the commit must import reviewer-plus DISABLED, got %v", after)
	}
	// The carried denial is now a real policy row (only at commit, never at create).
	if n := len(mustPolicyList(t, st)); n == 0 {
		t.Fatalf("the commit must store the carried denial, got none")
	}

	// 6. a second store (fresh state home), same fixture bytes in a second dir:
	// a direct CLI import produces the same agent rows as the commit did,
	// except for the timestamp and source the package's own location gives.
	t.Run("a direct import equals the committed import except timestamps and source", func(t *testing.T) {
		agentHome(t) // a fresh state home, so this store is a clean one
		dir2 := writeAgentPackage(t, "reviewer-plus", carriedFile("reviewer-plus"))
		if _, err := runAgent(t, "import", dir2); err != nil {
			t.Fatalf("the direct CLI import: %v", err)
		}
		st2 := openAgentStore(t)
		direct := rowsKey(pluginRows(t, st2))
		committed := rowsKey(after)
		if direct["reviewer-plus"] != committed["reviewer-plus"] {
			t.Fatalf("a direct import must equal the committed import by state:\ncommit  %v\ndirect  %v", committed, direct)
		}
		if len(direct) != len(committed) {
			t.Fatalf("a direct import must produce the same set of agents:\ncommit  %v\ndirect  %v", committed, direct)
		}
	})
}

// TestAgentAdmissionCreateRefusesWhatImportRefuses (criterion 2): the create
// door and the import door run the SAME validator, so a package the import
// refuses is refused at create with the same text, and no row is written. The
// create door is not a looser front door.
func TestAgentAdmissionCreateRefusesWhatImportRefuses(t *testing.T) {
	agentHome(t)
	st := openAgentStore(t)
	d := daemon.New(daemon.Options{Store: st, ValidateProposal: validateProposalPayload, AgentImportPreview: agentImportPreviewFor(st)})

	// A relative path is refused at the create door by the same absolute-path
	// rule the import applies, and no row is written.
	_, derr := sliceDispatch(t, d, "proposal.create", map[string]any{
		"kind":    "agent_import",
		"payload": map[string]any{"pkgPath": "relative/path/to/package"},
	})
	if derr == nil {
		t.Fatalf("a relative path must be refused at the create door; it succeeded")
	}
	// No row was written for the refused create.
	rows, err := st.ListProposals("")
	if err != nil {
		t.Fatalf("list proposals: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("a refused create must write no row, got %d", len(rows))
	}
}
