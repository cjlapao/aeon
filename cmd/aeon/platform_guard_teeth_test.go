package main

// platform_guard_teeth_test.go — S54 T3: every platform guard has a canary.
// Each predicate below is the SAME function the live guard runs, fed a fake
// input shaped like the offense it exists to catch; each clean control is
// what makes the offense list meaningful (a predicate that flags everything
// passes nothing). Naming follows the automation/autonomy teeth sets.

import (
	"regexp"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/narrow"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/records"
	"github.com/cjlapao/aeon/internal/tools"
)

func TestPlatformGuardsHaveTeeth(t *testing.T) {
	t.Run("narrow-projection", func(t *testing.T) {
		bad := narrowProjectingOpsOffenses([]narrow.Action{
			{Name: "aeon_graph_delete", SocketMethod: protocol.MethodGraphDelete},
		})
		if len(bad) != 1 || !strings.Contains(bad[0], "graph.delete") {
			t.Errorf("a narrow action projecting graph.delete slipped past: %v", bad)
		}
		if bad := narrowProjectingOpsOffenses([]narrow.Action{
			{Name: "aeon_graph_query", SocketMethod: protocol.MethodGraphQuery},
		}); len(bad) != 0 {
			t.Errorf("the read sibling graph.query was flagged — the predicate is not scoped to operations: %v", bad)
		}
	})

	t.Run("registry-and-catalog", func(t *testing.T) {
		if bad := registryOpsOffenses(map[string]tools.Tool{"graph_build": {}}); len(bad) != 1 {
			t.Errorf("a control twin in the registry slipped past: %v", bad)
		}
		if bad := registryOpsOffenses(tools.Registry()); len(bad) != 0 {
			t.Errorf("the REAL registry was flagged — the live guard's pass and this canary disagree: %v", bad)
		}
		if bad := catalogOpsOffenses([]records.Spec{{Family: "operations", Name: "retrieval_reindex"}}); len(bad) != 1 {
			t.Errorf("a control twin in the catalog slipped past: %v", bad)
		}
		if bad := catalogOpsOffenses([]records.Spec{{Family: "retrieval", Name: "index_status"}}); len(bad) != 0 {
			t.Errorf("the legitimate read twin was flagged: %v", bad)
		}
	})

	t.Run("agent-services", func(t *testing.T) {
		if bad := agentServicesOpsOffenses([]protocol.AgentServiceMethod{{Name: protocol.MethodGraphBuild}}); len(bad) != 1 {
			t.Errorf("an operation in the agent enumeration slipped past: %v", bad)
		}
	})

	t.Run("door-package-text", func(t *testing.T) {
		bad := platformWordOffenses("func kick() {\n\t// quietly calls retrieval.reindex\n}\n")
		if len(bad) != 1 {
			t.Errorf("operation vocabulary in an agent-facing package slipped past: %v", bad)
		}
		if bad := platformWordOffenses("func list() {\n\treturn readRows()\n}\n"); len(bad) != 0 {
			t.Errorf("innocent source was flagged: %v", bad)
		}
	})

	t.Run("start-record-writer", func(t *testing.T) {
		line := "\t\tid, err := st.InsertAsyncWorker(ctx, spec)\n"
		bad := startRecordWriteOffenses(map[string]string{"/tmp/x/internal/forge/rogue.go": line})
		if len(bad) != 1 || !strings.Contains(bad[0], "forge") {
			t.Errorf("a rogue start-record writer slipped past: %v", bad)
		}
		if bad := startRecordWriteOffenses(map[string]string{"/tmp/x/internal/asyncworkers/manager.go": line}); len(bad) != 0 {
			t.Errorf("the manager itself was flagged — the allow-list does not admit the one lawful caller: %v", bad)
		}
	})

	t.Run("ledger-mutation", func(t *testing.T) {
		fake := "func fix() {\n\tUPDATE platform_change SET summary='x'\n\t// DELETE FROM platform_change -- commented\n\ttx.ExecContext(ctx, \"DELETE FROM platform_change WHERE id=1\")\n}\n"
		bad := platformChangeMutationOffenses(map[string]string{"internal/daemon/bad.go": fake})
		if len(bad) != 2 {
			t.Errorf("ledger mutations slipped past (want UPDATE + DELETE, comments skipped): %v", bad)
		}
	})

	t.Run("second-status-computation", func(t *testing.T) {
		line := "\tpending, _ := idx.PendingChunkCount(root, corpus)\n"
		bad := statusComputeOffenses(map[string]string{"internal/forge/rogue.go": line})
		if len(bad) != 1 {
			t.Errorf("a second computation of the retrieval figure slipped past: %v", bad)
		}
		if bad := statusComputeOffenses(map[string]string{"internal/retrieval/reindex.go": line}); len(bad) != 0 {
			t.Errorf("the owning package was flagged: %v", bad)
		}
		panelBad := panelFileComputeOffenses(map[string]string{
			"a/internal/daemon/platform_reads.go": "\tst.db.Select(\"SELECT 1\")\n",
		})
		if len(panelBad) != 1 {
			t.Errorf("SQL smuggled into the panel file slipped past: %v", panelBad)
		}
	})

	t.Run("unwired-render", func(t *testing.T) {
		bad := unwiredRenderOffenses(`{"retrieval":{"state":"unwired","corpora":[]}}`)
		if len(bad) != 1 {
			t.Errorf("an unwired block rendering an empty list slipped past: %v", bad)
		}
		if bad := unwiredRenderOffenses(`{"retrieval":{"state":"unwired","reason":"this build has no retrieval plane"}}`); len(bad) != 0 {
			t.Errorf("the honest unwired rendering was flagged: %v", bad)
		}
	})

	t.Run("pin-extractor", func(t *testing.T) {
		// The extractor must reproduce a pinned digest exactly — proving the
		// recipe is the one T0 hashed with — and must refuse a function that
		// is not there rather than hashing the whole file.
		body, ok := pinnedBody("cmd/aeon/tools_guard_test.go", "TestEveryMethodIsClassified")
		if !ok {
			t.Fatal("the extractor could not find a function that is in the file")
		}
		if !strings.HasPrefix(body, "func TestEveryMethodIsClassified(") || !strings.HasSuffix(body, "}\n") {
			t.Error("the extraction window is not signature-through-closing-brace")
		}
		if got := pinnedDigest(body); got != "8703f5545d0f0b69" {
			t.Errorf("the extractor's recipe disagrees with the pin it checks: got %s", got)
		}
		if _, ok := pinnedBody("cmd/aeon/tools_guard_test.go", "TestNoSuchPinExists"); ok {
			t.Error("the extractor found a function that does not exist")
		}
	})

	t.Run("word-vocabulary-is-not-merely-one-name", func(t *testing.T) {
		// The words list carries the tool-shaped spellings too, so a future
		// control tool cannot dodge the dotted form.
		re := regexp.MustCompile(`graph_build`)
		hit := false
		for _, w := range platformOpWords {
			if re.MatchString(w) {
				hit = true
			}
		}
		if !hit || len(platformWordOffenses("var _ = \"graph_build\"")) != 1 {
			t.Error("the underscore spelling of an operation is not in the guarded vocabulary")
		}
	})

	t.Run("read-words-are-scoped-not-global", func(t *testing.T) {
		// T4's correction in force: the READ names are refused where reads
		// do not belong, and the catalog twin line must NOT be flagged by
		// the operation predicate — the predicate that runs over internal/
		// records, where the twin legitimately names its socket method.
		if len(platformReadWordOffenses("const m = \"platform.status\"")) != 1 {
			t.Error("a read name slipped past the strict-surface predicate")
		}
		if bad := platformWordOffenses("{Name: \"index_status\", Method: \"platform.status\", Family: \"platform\"}"); len(bad) != 0 {
			t.Errorf("the legitimate catalog twin line was flagged as an operation: %v", bad)
		}
	})
}
