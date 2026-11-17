package store

import (
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/model"
)

// S18 T3 at the store: the probe cache. What a probe EARNED survives a
// restart; what it merely DECLARED does not exist as a row — absence is the
// representation (I5), and the schema's CHECK is the proof that a `declared`
// badge cannot be written even by someone holding the SQL.

func verdict(c model.Capability, state model.CapabilityState, detail string) model.ProbeResult {
	return model.ProbeResult{
		Capability: c, State: state, Detail: detail,
		ProbedAt: time.Date(2026, 10, 26, 9, 0, 0, 0, time.UTC),
		Model:    "llama3.2:1b",
	}
}

func mustVerdicts(t *testing.T, s *Store, routeID string) map[model.Capability]model.ProbeResult {
	t.Helper()
	rs, err := s.ProbeResults(routeID)
	if err != nil {
		t.Fatalf("read probes of %q: %v", routeID, err)
	}
	m := map[model.Capability]model.ProbeResult{}
	for _, r := range rs {
		m[r.Capability] = r
	}
	return m
}

// TestDeclaredIsTheAbsenceOfARow — the store never writes the third state.
// A probe returns declared verdicts for display; the cache holds only what
// was earned, and a never-probed route reads back as nothing, not as rows.
func TestDeclaredIsTheAbsenceOfARow(t *testing.T) {
	s := openTemp(t)
	if err := s.UpsertRoute(unmetered(bareRoute("probe-me"))); err != nil {
		t.Fatal(err)
	}

	got := mustVerdicts(t, s, "probe-me")
	if len(got) != 0 {
		t.Fatalf("a never-probed route has %d cached verdicts; absence must be declared", len(got))
	}

	all := []model.ProbeResult{
		verdict(model.CapTools, model.StateVerified, "accepted the tool declaration"),
		verdict(model.CapStreamingUsage, model.StateContradicted, "no usage in the stream"),
		verdict(model.CapThinking, model.StateDeclared, "answered without thinking — proves nothing"),
		verdict(model.CapSystemRole, model.StateDeclared, "not cheaply probable"),
		verdict(model.CapParallelTools, model.StateDeclared, "one request, no refusal seen"),
	}
	if err := s.PutProbeResults("probe-me", all); err != nil {
		t.Fatal(err)
	}
	got = mustVerdicts(t, s, "probe-me")
	if len(got) != 2 {
		t.Fatalf("%d rows for two earned verdicts and three declared ones: %v", len(got), got)
	}
	if got[model.CapThinking].State != "" {
		t.Error("a declared verdict was STORED — I5's rule rendered as a row is the badge-invention the schema exists to refuse")
	}
	if got[model.CapTools].State != model.StateVerified || got[model.CapStreamingUsage].State != model.StateContradicted {
		t.Errorf("earned verdicts did not round-trip: %v", got)
	}
	if got[model.CapStreamingUsage].Detail != "no usage in the stream" {
		t.Errorf("detail lost: %q", got[model.CapStreamingUsage].Detail)
	}
	if !got[model.CapTools].ProbedAt.Equal(time.Date(2026, 10, 26, 9, 0, 0, 0, time.UTC)) {
		t.Errorf("probed_at did not survive: %v", got[model.CapTools].ProbedAt)
	}
	if got[model.CapTools].Model != "llama3.2:1b" {
		t.Errorf("probed model lost: %q (O2)", got[model.CapTools].Model)
	}
}

// TestTheSchemaRefusesADeclaredRow — the CHECK is the enforcement, not
// etiquette. Someone holding the SQL directly still cannot write the badge.
func TestTheSchemaRefusesADeclaredRow(t *testing.T) {
	s := openTemp(t)
	_, err := s.w.Exec(`INSERT INTO route_probe (route_id, capability, state, detail, probed_model, probed_at)
VALUES ('x', 'tools', 'declared', '', 'm', '2026-10-26T09:00:00Z')`)
	if err == nil {
		t.Fatal("the schema let a `declared` row be written — declared must be unwritable, not merely un-written-by-convention")
	}
	if !strings.Contains(err.Error(), "CHECK") {
		t.Errorf("refused for the wrong reason: %v", err)
	}
}

// TestAReprobeReplacesTheVerdicts — the record reflects the LATEST probe.
// A capability contradicted in September and verified in October must not
// keep refusing steps on the September claim, and a wipe cannot be partial:
// the replace is one transaction (a half-applied replace would silently
// unblock a route that should be refused).
func TestAReprobeReplacesTheVerdicts(t *testing.T) {
	s := openTemp(t)
	if err := s.UpsertRoute(unmetered(bareRoute("flip"))); err != nil {
		t.Fatal(err)
	}
	if err := s.PutProbeResults("flip", []model.ProbeResult{
		verdict(model.CapTools, model.StateContradicted, "the 400 said no tools"),
		verdict(model.CapStreamingUsage, model.StateContradicted, "no usage"),
	}); err != nil {
		t.Fatal(err)
	}
	later := verdict(model.CapTools, model.StateVerified, "this time it accepted tools")
	later.ProbedAt = time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC)
	if err := s.PutProbeResults("flip", []model.ProbeResult{later}); err != nil {
		t.Fatal(err)
	}
	got := mustVerdicts(t, s, "flip")
	if len(got) != 1 {
		t.Fatalf("the September contradictions outlived the October probe: %v", got)
	}
	if got[model.CapTools].State != model.StateVerified || !strings.Contains(got[model.CapTools].Detail, "this time") {
		t.Errorf("latest probe did not replace: %+v", got[model.CapTools])
	}
}

// TestAnUnageableVerdictIsNotCached — O2 in the store's own words: a
// verdict without a timestamp or without a probed model cannot be judged
// for staleness or substitution, so it does not belong in the cache.
func TestAnUnageableVerdictIsNotCached(t *testing.T) {
	s := openTemp(t)
	if err := s.UpsertRoute(unmetered(bareRoute("ageless"))); err != nil {
		t.Fatal(err)
	}
	noTime := verdict(model.CapTools, model.StateVerified, "sure")
	noTime.ProbedAt = time.Time{}
	if err := s.PutProbeResults("ageless", []model.ProbeResult{noTime}); err == nil {
		t.Fatal("cached a verdict that cannot be aged")
	}
	noModel := verdict(model.CapTools, model.StateVerified, "sure")
	noModel.Model = "   "
	if err := s.PutProbeResults("ageless", []model.ProbeResult{noModel}); err == nil {
		t.Fatal("cached a verdict that cannot say which model it spoke for")
	}
	if got := mustVerdicts(t, s, "ageless"); len(got) != 0 {
		t.Error("a refused Put must have written nothing")
	}

	unknown := verdict(model.Capability("vision"), model.StateVerified, "nope")
	if err := s.PutProbeResults("ageless", []model.ProbeResult{unknown}); err == nil {
		t.Fatal("cached a verdict for a capability outside the Q8 set")
	}
}

// TestVerdictsReadBackInCanonicalOrder — rows arrive in insertion order;
// surfaces read canonical order, the same P31 discipline as declarations.
func TestVerdictsReadBackInCanonicalOrder(t *testing.T) {
	s := openTemp(t)
	if err := s.UpsertRoute(unmetered(bareRoute("order"))); err != nil {
		t.Fatal(err)
	}
	if err := s.PutProbeResults("order", []model.ProbeResult{
		verdict(model.CapParallelTools, model.StateVerified, "two calls"),
		verdict(model.CapStreamingUsage, model.StateVerified, "usage came"),
		verdict(model.CapTools, model.StateVerified, "accepted"),
	}); err != nil {
		t.Fatal(err)
	}
	rs, err := s.ProbeResults("order")
	if err != nil {
		t.Fatal(err)
	}
	want := []model.Capability{model.CapTools, model.CapStreamingUsage, model.CapParallelTools}
	for i, c := range want {
		if rs[i].Capability != c {
			t.Fatalf("verdict %d is %s, want canonical %s (row order must never reach a surface)", i, rs[i].Capability, c)
		}
	}
}

// TestDeletingARouteDeletesItsVerdicts — and a NEW route reusing the id
// must not inherit the old route's claims. Probe rows are the old server's
// evidence about the old server.
func TestDeletingARouteDeletesItsVerdicts(t *testing.T) {
	s := openTemp(t)
	if err := s.UpsertRoute(unmetered(bareRoute("recycled"))); err != nil {
		t.Fatal(err)
	}
	if err := s.PutProbeResults("recycled", []model.ProbeResult{
		verdict(model.CapTools, model.StateContradicted, "the old server refused tools"),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DeleteRoute("recycled"); err != nil {
		t.Fatal(err)
	}
	if got := mustVerdicts(t, s, "recycled"); len(got) != 0 {
		t.Fatalf("verdicts outlived their route: %v", got)
	}
	// Re-create the id: a fresh route starts with no earned verdicts.
	if err := s.UpsertRoute(unmetered(bareRoute("recycled"))); err != nil {
		t.Fatal(err)
	}
	if got := mustVerdicts(t, s, "recycled"); len(got) != 0 {
		t.Fatalf("the new 'recycled' inherited the old server's contradiction: %v — every step would refuse for a server it never was", got)
	}
}

// TestVerdictsSurviveAReopen — the whole reason the cache is a table.
// A daemon that forgets its probes on restart returns every route to
// `declared` and loses the contradiction protecting Friday's loop.
func TestVerdictsSurviveAReopen(t *testing.T) {
	dir := t.TempDir()
	s := openAt(t, dir)
	if err := s.UpsertRoute(unmetered(bareRoute("persist"))); err != nil {
		t.Fatal(err)
	}
	if err := s.PutProbeResults("persist", []model.ProbeResult{
		verdict(model.CapStreamingUsage, model.StateContradicted, "usage never came"),
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	again := openAt(t, dir)
	got := mustVerdicts(t, again, "persist")
	if got[model.CapStreamingUsage].State != model.StateContradicted {
		t.Fatalf("after reopen, streaming-usage is %v — the contradiction that protects the loop vanished with the process", got[model.CapStreamingUsage].State)
	}
}
