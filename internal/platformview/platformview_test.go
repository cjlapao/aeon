package platformview

// S54 T2 — the composer is tested as the pure function it is: figures in,
// panel out, no seams to fake and no clock to catch. The claims: every block
// equals its input (differential), unwired is exactly the nil seam, a failed
// read carries the source's sentence verbatim and nothing else, and the
// graph block's stale is ONLY "never built" — the finding T0 measured and
// this package's comment promises.

import "testing"

func TestBlocksEqualTheirSources(t *testing.T) {
	// SP1's whole claim at the pure level: the panel carries the figures
	// the sources answered, field for field. A composed read that
	// recomputed anything would drift one input change at a time; drift
	// fails here first.
	in := Inputs{
		EvaluatedAt: "2026-11-04T14:31:07Z",
		Retrieval: []*RetrievalInput{{
			Root: "/srv/a", EmbedRoute: "openai-embed", EmbedModel: "emb-small", EmbedDim: 1536,
			Corpora: []CorpusInput{
				{Name: "code", Files: 1204, StaleFiles: 38, PendingEmbed: 96, Available: true},
				{Name: "history", Files: 71, Available: true},
			},
		}},
		Graph: &GraphInput{Roots: []GraphRootInput{{Root: "/srv/a", Built: true, Files: 431, Nodes: 18342, Edges: 60719}}},
		Usage: &UsageInput{LoopsRun: 12, InputTokensTotal: 481234, OutputTokensTotal: 90211, CostUSDTotal: 4.17, CostState: "known", Exists: true},
	}
	v := Compose(in)
	if v.EvaluatedAt != in.EvaluatedAt {
		t.Errorf("evaluatedAt = %q, want the caller's stamp %q", v.EvaluatedAt, in.EvaluatedAt)
	}
	if len(v.Retrieval.Components) != 1 {
		t.Fatalf("components = %d, want 1", len(v.Retrieval.Components))
	}
	c := v.Retrieval.Components[0]
	if c.Component != "/srv/a" || c.EmbedRoute != "openai-embed" || c.EmbedModel != "emb-small" || c.EmbedDim != 1536 {
		t.Errorf("identity line drifted from its source: %+v", c)
	}
	if len(c.Corpora) != 2 || c.Corpora[0].Name != "code" || c.Corpora[0].Files != 1204 ||
		c.Corpora[0].StaleChunks != 96 || c.Corpora[1].Name != "history" || c.Corpora[1].Files != 71 {
		t.Errorf("corpus figures drifted from the source's: %+v", c.Corpora)
	}
	if c.Corpora[0].StaleChunks != in.Retrieval[0].Corpora[0].PendingEmbed {
		t.Error("staleChunks is not the pending-embed count — a recomputation appeared")
	}
	g := v.Graph.Roots[0]
	if g.Root != "/srv/a" || g.NeverBuilt || g.Files != 431 || g.Nodes != 18342 || g.Edges != 60719 {
		t.Errorf("graph figures drifted from the status's: %+v", g)
	}
	u := v.Usage.Workspace
	if u.LoopsRun != 12 || u.InputTokensTotal != 481234 || u.OutputTokensTotal != 90211 ||
		u.CostUSDTotal != 4.17 || u.CostState != "known" || !u.Recorded {
		t.Errorf("usage figures drifted from the singleton row: %+v", u)
	}
	if v.Retrieval.State != StateStale {
		t.Errorf("retrieval state = %q, want stale (38 stale files, 96 pending)", v.Retrieval.State)
	}
	if v.Graph.State != StateAvailable || v.Usage.State != StateAvailable {
		t.Errorf("states drifted: graph %q usage %q", v.Graph.State, v.Usage.State)
	}
}

func TestUnwiredExactlyWhenTheSeamIsAbsent(t *testing.T) {
	// V13 in the panel's words: nil input is the ABSENT seam — unwired,
	// with no components list, no roots and no figures. An empty slice is
	// NOT unwired: it is a wired subsystem with nothing indexed, and it
	// must not share the absent block's answer.
	v := Compose(Inputs{})
	if v.Retrieval.State != StateUnwired || v.Graph.State != StateUnwired || v.Usage.State != StateUnwired {
		t.Fatalf("empty inputs answered %q / %q / %q — every block must say unwired",
			v.Retrieval.State, v.Graph.State, v.Usage.State)
	}
	if v.Retrieval.Components != nil || v.Graph.Roots != nil || v.Usage.Workspace != nil {
		t.Error("an unwired block carried figures — the absent seam dressed as an empty one")
	}
	// The other half: wired-and-empty answers available with no list, the
	// different fact kept distinguishable.
	v2 := Compose(Inputs{Retrieval: []*RetrievalInput{}})
	if v2.Retrieval.State != StateAvailable {
		t.Errorf("wired-and-empty answered %q, want available (nothing indexed is not nothing wired)", v2.Retrieval.State)
	}
}

func TestErrorCarriesTheSourceSentenceVerbatim(t *testing.T) {
	// The one-formatter law: a refusal rides quoted, not paraphrased —
	// and one failed read fails the BLOCK (the panel never hides a
	// half-refusing subsystem behind the roots that did answer).
	sentence := "read status for /srv/a: index walk refused: open /x: permission denied"
	v := Compose(Inputs{Retrieval: []*RetrievalInput{{
		Root: "/srv/a", Corpora: []CorpusInput{{Name: "code", Files: 9, Available: true}},
	}, {Root: "/srv/b", Err: sentence}}})
	if v.Retrieval.State != StateError || v.Retrieval.Error != sentence {
		t.Errorf("retrieval answered %q / %q, want error + the exact sentence %q",
			v.Retrieval.State, v.Retrieval.Error, sentence)
	}
	if len(v.Retrieval.Components) != 0 {
		t.Error("the error block still carried the roots that answered — a half-truth panel")
	}
	vg := Compose(Inputs{Graph: &GraphInput{Err: "graph roots read refused: database is locked"}})
	if vg.Graph.State != StateError || vg.Graph.Error != "graph roots read refused: database is locked" {
		t.Errorf("graph error block = %+v", vg.Graph)
	}
	vu := Compose(Inputs{Usage: &UsageInput{Err: "telemetry row unreadable"}})
	if vu.Usage.State != StateError || vu.Usage.Error != "telemetry row unreadable" || vu.Usage.Workspace != nil {
		t.Errorf("usage error block = %+v", vu.Usage)
	}
}

func TestBuildingBeatsStaleAndFlyingIsTheLedgersFact(t *testing.T) {
	// "building" is the ledger's flying row (AnyPassFlying) or the
	// corpus's own reindexing flag — never a guess — and it outranks
	// stale, because "a pass is flying" is the more urgent truth about
	// the same staleness.
	v := Compose(Inputs{Retrieval: []*RetrievalInput{{
		Root: "/srv/a", AnyPassFlying: true,
		Corpora: []CorpusInput{{Name: "code", Files: 9, StaleFiles: 3, PendingEmbed: 3, Available: true}},
	}}})
	if v.Retrieval.State != StateBuilding {
		t.Errorf("flying pass answered %q, want building", v.Retrieval.State)
	}
	v2 := Compose(Inputs{Retrieval: []*RetrievalInput{{
		Root:    "/srv/a",
		Corpora: []CorpusInput{{Name: "code", Files: 9, Available: true, Reindexing: true}},
	}}})
	if v2.Retrieval.State != StateBuilding {
		t.Errorf("corpus-flagged reindex answered %q, want building", v2.Retrieval.State)
	}
	vg := Compose(Inputs{Graph: &GraphInput{Building: true, Roots: []GraphRootInput{{Root: "/a"}}}})
	if vg.Graph.State != StateBuilding {
		t.Errorf("flying graph build answered %q, want building", vg.Graph.State)
	}
}

func TestGraphStaleMeansOnlyNeverBuilt(t *testing.T) {
	// T0's finding, pinned: the graph plane computes NO staleness today,
	// so the panel's stale may mean only "listed but never built" — the
	// zero rows are the source's own fact, not a hunch. A built root with
	// edges is available even beside a never-built one, and NEVER stale
	// on its own.
	v := Compose(Inputs{Graph: &GraphInput{Roots: []GraphRootInput{
		{Root: "/built", Built: true, Files: 40, Nodes: 900, Edges: 2200},
		{Root: "/never", Built: false},
	}}})
	if v.Graph.State != StateStale {
		t.Errorf("state = %q, want stale (one root was listed and never built)", v.Graph.State)
	}
	if !v.Graph.Roots[1].NeverBuilt || v.Graph.Roots[0].NeverBuilt {
		t.Errorf("neverBuilt flags wrong: %+v", v.Graph.Roots)
	}
	v2 := Compose(Inputs{Graph: &GraphInput{Roots: []GraphRootInput{{Root: "/built", Built: true, Files: 1}}}})
	if v2.Graph.State != StateAvailable {
		t.Errorf("an all-built panel answered %q — no staleness source exists for the graph block", v2.Graph.State)
	}
}

func TestAvailabilitySentenceQuotedFromTheLeaf(t *testing.T) {
	// The route half of the retrieval block, worded exactly as the
	// retrieval leaf words it (reindex.go priceDrift's own sentence) —
	// paraphrase here would be a second voice for the same fact.
	v := Compose(Inputs{Retrieval: []*RetrievalInput{{
		Root: "/srv/a", EmbedModel: "", EmbedDim: 0, // no route at all
		Corpora: []CorpusInput{{Name: "code", Files: 9, Available: true}},
	}}})
	if v.Retrieval.Availability != "no embedding route to price the pass" {
		t.Errorf("availability = %q, want the leaf's sentence verbatim", v.Retrieval.Availability)
	}
	v2 := Compose(Inputs{Retrieval: []*RetrievalInput{{
		Root: "/srv/a", EmbedRoute: "openai-embed", EmbedModel: "emb", EmbedDim: 8,
		Corpora: []CorpusInput{{Name: "code", Files: 9, Available: true}},
	}}})
	if v2.Retrieval.State != StateAvailable || v2.Retrieval.Availability != "" {
		t.Errorf("a routed panel answered %+v", v2.Retrieval)
	}
}

func TestUsageUnrecordedIsDataNotError(t *testing.T) {
	// DB19's fourth state: exists:false is "nothing recorded yet", which
	// is neither an error nor a history of zeroes pretending otherwise.
	v := Compose(Inputs{Usage: &UsageInput{Exists: false}})
	if v.Usage.State != StateAvailable || v.Usage.Workspace == nil || v.Usage.Workspace.Recorded {
		t.Errorf("unrecorded usage answered %+v", v.Usage)
	}
	if v.Usage.Workspace.LoopsRun != 0 || v.Usage.Workspace.CostUSDTotal != 0 {
		t.Error("figures were fabricated for a workspace that recorded nothing")
	}
}
