package store

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/model"
)

// The `route` table, and the one property that matters more than persistence
// itself: the credential is not in it.

func TestARouteSurvivesACloseAndReopen(t *testing.T) {
	// The promise S3 made and S4 first shipped without. A person configuring a
	// route in the app watched it vanish on restart.
	dir := t.TempDir()

	first := openAt(t, dir)
	if err := first.UpsertRoute(model.Route{
		ID:              "anthropic",
		Provider:        "anthropic",
		Model:           "claude-sonnet-5",
		BaseURL:         "https://api.anthropic.com/v1",
		MaxOutputTokens: 8192,
		Prices: &model.Prices{
			InputUSDPerMTok:  model.Float64Ptr(3),
			OutputUSDPerMTok: model.Float64Ptr(15),
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	second := openAt(t, dir)
	routes, err := second.ListRoutes()
	if err != nil {
		t.Fatal(err)
	}
	if len(routes) != 1 {
		t.Fatalf("after a reopen there are %d routes, want 1", len(routes))
	}
	r := routes[0]
	if r.ID != "anthropic" || r.Provider != "anthropic" || r.Model != "claude-sonnet-5" {
		t.Errorf("the route came back as %+v", r)
	}
	if r.BaseURL != "https://api.anthropic.com/v1" {
		t.Errorf("the base URL came back as %q", r.BaseURL)
	}
	if r.MaxOutputTokens != 8192 {
		t.Errorf("the output cap came back as %d", r.MaxOutputTokens)
	}
	if r.Prices == nil || r.Prices.InputUSDPerMTok == nil || *r.Prices.InputUSDPerMTok != 3 {
		t.Errorf("the prices came back as %+v", r.Prices)
	}
	if *r.Prices.OutputUSDPerMTok != 15 {
		t.Errorf("the output price came back as %v", *r.Prices.OutputUSDPerMTok)
	}
}

func TestNoAPIKeyEverReachesTheDatabase(t *testing.T) {
	// C17: a credential comes from the environment or a dedicated file path,
	// never a config file. The store is neither.
	//
	// Asserted on the FILE's RAW BYTES, not on the absence of a column. A
	// column could be added later, or a key could reach another column by
	// accident — a prices blob, an extra_body, a document. This reads the whole
	// database and looks for the string.
	dir := t.TempDir()
	s := openAt(t, dir)

	const secret = "sk-ant-api03-THIS-MUST-NOT-BE-ON-DISK-7f3a"

	// Handed in deliberately, the way a careless caller would.
	if err := s.UpsertRoute(unmetered(model.Route{
		ID:       "anthropic",
		Provider: "anthropic",
		Model:    "claude-sonnet-5",
		APIKey:   secret,
	})); err != nil {
		t.Fatal(err)
	}
	// And through everything else that takes free text, so this test covers
	// more than the one function.
	sc := seedLoop(t, s, "keys", "/repo")
	if _, err := s.WriteDocument(sc, "plan", "a plan mentioning no secret", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AppendEvent(NewEvent{Kind: "route.set", Payload: map[string]any{"id": "anthropic"}}); err != nil {
		t.Fatal(err)
	}

	// Checkpointed, so the bytes are in the file rather than only in the WAL.
	if _, err := s.w.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{s.Path(), s.Path() + "-wal"} {
		raw, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), secret) {
			t.Errorf("the API key is in %s. C17 allows the environment or a dedicated file path and "+
				"nothing else — and a key in the database is a key that gets copied with a backup, "+
				"read by anything that can open the file, and migrated by every future schema change", path)
		}
		// The prefix alone is enough to fail: a partial write is still a leak.
		if strings.Contains(string(raw), "sk-ant-api03") {
			t.Errorf("a provider key prefix appears in %s", path)
		}
	}

	// And the route is otherwise intact, so this test cannot pass by the store
	// having refused the write.
	routes, err := s.ListRoutes()
	if err != nil {
		t.Fatal(err)
	}
	if len(routes) != 1 || routes[0].Model != "claude-sonnet-5" {
		t.Fatalf("the route was not stored at all: %+v", routes)
	}
	if routes[0].APIKey != "" {
		t.Errorf("ListRoutes returned a key (%q); there is nowhere it could have come from",
			routes[0].APIKey)
	}
}

func TestTheRouteTableHasNoColumnThatCouldHoldAKey(t *testing.T) {
	// The structural half of the test above. The byte scan catches a key that
	// got in; this catches a COLUMN that invites one — which is the change
	// somebody would make in good faith while adding a feature.
	s := openTemp(t)
	rows, err := s.r.Query(`SELECT name FROM pragma_table_info('route')`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	var columns []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		columns = append(columns, n)

		// Matched on whole NAME PARTS, not substrings. The first version of
		// this test used `strings.Contains` and flagged `max_output_tokens`,
		// which is a model's output budget and not a credential — and a guard
		// that cries wolf is a guard somebody deletes.
		//
		// The distinction is exact: `token` singular is a credential
		// (`api_token`, `access_token`); `tokens` plural is what a model
		// counts.
		forbidden := map[string]bool{
			"key": true, "secret": true, "token": true,
			"password": true, "credential": true, "apikey": true, "auth": true,
		}
		for _, part := range strings.Split(strings.ToLower(n), "_") {
			if forbidden[part] {
				t.Errorf("route.%s looks like it holds a credential (the %q part). C17 says the "+
					"environment or a dedicated file path — the credential belongs in "+
					"internal/secrets, not here", n, part)
			}
		}
	}
	// The negative control: the matcher must still catch a real one.
	for _, name := range []string{"api_key", "access_token", "secret", "auth_header"} {
		caught := false
		forbidden := map[string]bool{
			"key": true, "secret": true, "token": true,
			"password": true, "credential": true, "apikey": true, "auth": true,
		}
		for _, part := range strings.Split(name, "_") {
			if forbidden[part] {
				caught = true
			}
		}
		if !caught {
			t.Errorf("the matcher would not catch a column called %q, so it is not guarding anything", name)
		}
	}

	if len(columns) < 7 {
		t.Fatalf("the route table has %d columns (%v), which is too few to be the real one",
			len(columns), columns)
	}
}

func TestUpsertRouteReplacesRatherThanDuplicating(t *testing.T) {
	s := openTemp(t)
	for _, m := range []string{"claude-sonnet-5", "claude-opus-5"} {
		if err := s.UpsertRoute(unmetered(model.Route{ID: "anthropic", Provider: "anthropic", Model: m})); err != nil {
			t.Fatal(err)
		}
	}
	routes, err := s.ListRoutes()
	if err != nil {
		t.Fatal(err)
	}
	if len(routes) != 1 {
		t.Fatalf("setting the same id twice produced %d routes", len(routes))
	}
	if routes[0].Model != "claude-opus-5" {
		t.Errorf("the second write did not replace the first: %q", routes[0].Model)
	}
}

func TestDeleteRouteReportsWhetherOneExisted(t *testing.T) {
	// The caller needs to tell "deleted" from "there was nothing", because the
	// app renders them differently and a credential file may still need
	// removing either way.
	s := openTemp(t)
	if err := s.UpsertRoute(unmetered(model.Route{ID: "openai", Provider: "openai", Model: "gpt-5"})); err != nil {
		t.Fatal(err)
	}
	existed, err := s.DeleteRoute("openai")
	if err != nil {
		t.Fatal(err)
	}
	if !existed {
		t.Error("deleting an existing route reported that nothing existed")
	}
	existed, err = s.DeleteRoute("openai")
	if err != nil {
		t.Fatal(err)
	}
	if existed {
		t.Error("deleting a missing route reported that something existed")
	}
}

func TestARouteNeedsAnIDAProviderAndAModel(t *testing.T) {
	// Refused at the store, as well as at the protocol. A row with no provider
	// is a route that cannot be called, and it would read back as configured.
	s := openTemp(t)
	for _, r := range []model.Route{
		{Provider: "anthropic", Model: "m"},
		{ID: "x", Model: "m"},
		{ID: "x", Provider: "anthropic"},
		{ID: "  ", Provider: "anthropic", Model: "m"},
	} {
		if err := s.UpsertRoute(r); err == nil {
			t.Errorf("an incomplete route was stored: %+v", r)
		}
	}
}

func TestARouteWithNoPricesReadsBackWithNone(t *testing.T) {
	// "No prices" is a real state: it is what `priced: false` means in the app,
	// and what makes a cost `unknown` rather than wrong. An empty Prices struct
	// would make an unpriced route look priced at zero.
	//
	// Since S18 T2 the absence must be DECLARED (O8): the fixture says
	// NoPrice, which is exactly the distinction the rule exists for — this
	// route was decided unmetered, and reads back with no prices rather than
	// prices at zero.
	s := openTemp(t)
	if err := s.UpsertRoute(unmetered(model.Route{ID: "local", Provider: "openai", Model: "llama"})); err != nil {
		t.Fatal(err)
	}
	routes, err := s.ListRoutes()
	if err != nil {
		t.Fatal(err)
	}
	if routes[0].Prices != nil {
		t.Errorf("an unpriced route came back with prices %+v", routes[0].Prices)
	}
}

// `reasoning_effort` survives a restart, which is the whole reason it is a
// column rather than a flag somebody re-types.
//
// # Why this matters more than it looks
//
// Without it, a route that can run steps today cannot tomorrow: the daemon
// restarts, the effort is gone, the model's default comes back, and every step
// fails with a provider 400 about a field nobody touched.
func TestAReasoningEffortSurvivesAReopen(t *testing.T) {
	dir := t.TempDir()
	s := openAt(t, dir)

	if err := s.UpsertRoute(unmetered(model.Route{
		ID: "openai", Provider: "openai", Model: "gpt-6-luna",
		MaxOutputTokens: 4096, ReasoningEffort: "none",
	})); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	reopened := openAt(t, dir)
	back, err := reopened.ReadRoute("openai")
	if err != nil {
		t.Fatal(err)
	}
	if back.ReasoningEffort != "none" {
		t.Errorf("the reasoning effort came back as %q", back.ReasoningEffort)
	}
}

func TestAnUnsetReasoningEffortReadsBackEmptyRatherThanAWord(t *testing.T) {
	// Empty means "the provider decides". A stored "default" would be Aeon
	// claiming to have chosen something.
	s := openTemp(t)
	if err := s.UpsertRoute(unmetered(model.Route{
		ID: "anthropic", Provider: "anthropic", Model: "claude-sonnet-5", MaxOutputTokens: 4096,
	})); err != nil {
		t.Fatal(err)
	}
	back, err := s.ReadRoute("anthropic")
	if err != nil {
		t.Fatal(err)
	}
	if back.ReasoningEffort != "" {
		t.Errorf("an unset reasoning effort read back as %q", back.ReasoningEffort)
	}
}

func TestUpdatingARouteCanChangeItsReasoningEffort(t *testing.T) {
	// The upsert path, which is how `routes set` edits an existing route.
	s := openTemp(t)
	base := unmetered(model.Route{ID: "r", Provider: "openai", Model: "m", MaxOutputTokens: 4096})

	if err := s.UpsertRoute(base); err != nil {
		t.Fatal(err)
	}
	base.ReasoningEffort = "none"
	if err := s.UpsertRoute(base); err != nil {
		t.Fatal(err)
	}
	back, err := s.ReadRoute("r")
	if err != nil {
		t.Fatal(err)
	}
	if back.ReasoningEffort != "none" {
		t.Errorf("the update did not take: %q", back.ReasoningEffort)
	}

	// And back to unset, which must actually clear the column rather than keep
	// the old value — `--clear-reasoning-effort` depends on it.
	base.ReasoningEffort = ""
	if err := s.UpsertRoute(base); err != nil {
		t.Fatal(err)
	}
	if back, err = s.ReadRoute("r"); err != nil {
		t.Fatal(err)
	}
	if back.ReasoningEffort != "" {
		t.Errorf("clearing the reasoning effort left %q", back.ReasoningEffort)
	}
}

func TestRouteContextWindowPersistsForCompaction(t *testing.T) {
	s := openTemp(t)
	r := unmetered(model.Route{ID: "bounded", Provider: "anthropic", Model: "m", MaxOutputTokens: 1024, ContextWindow: 16384})
	if err := s.UpsertRoute(r); err != nil {
		t.Fatal(err)
	}
	back, err := s.ReadRoute(r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if back.ContextWindow != 16384 {
		t.Fatalf("context window after read = %d", back.ContextWindow)
	}
	all, err := s.ListRoutes()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 || all[0].ContextWindow != 16384 {
		t.Fatalf("context window after list: %+v", all)
	}
}

func TestModelMetadataSurvivesStoreReopen(t *testing.T) {
	dir := t.TempDir()
	first := openAt(t, dir)
	routes := []model.Route{
		{ID: "declared", Provider: "openai", Model: "slug", DisplayName: "Display", DisplayDescription: "Description", Icon: "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+/nWQAAAAASUVORK5CYII=", Capabilities: []string{"text", "thinking-effort"}, ReasoningEfforts: []string{"low", "max"}, ReasoningEffort: "max", MaxOutputTokens: 4096, NoPrice: true},
		{ID: "empty", Provider: "openai", Model: "slug", Capabilities: []string{}, ReasoningEfforts: []string{}, MaxOutputTokens: 4096, NoPrice: true},
		{ID: "legacy", Provider: "openai", Model: "slug", MaxOutputTokens: 4096, NoPrice: true},
	}
	for _, r := range routes {
		if err := first.UpsertRoute(r); err != nil {
			t.Fatal(err)
		}
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	second := openAt(t, dir)
	for _, original := range routes {
		got, err := second.ReadRoute(original.ID)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(original, got) {
			t.Fatalf("metadata changed across reopen: got %+v, want %+v", got, original)
		}
	}
}
