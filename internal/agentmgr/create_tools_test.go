package agentmgr

import (
	"context"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/agentcard"
	"github.com/cjlapao/aeon/internal/store"
)

var createActor = store.AgentActor{Kind: store.AgentActorUser, ID: "1000"}

func newCreateManager(t *testing.T) (*Manager, *store.Store) {
	t.Helper()
	s := openAgentStore(t)
	return NewManager(s, fakeVerifier(agentcard.CardStateVerified), nil, boolOverride(false)), s
}

// A name alone makes the agent it always made: no prompt, no tech, no tools, the default minion.
func TestCreateWithANameAloneIsWhatItAlwaysWas(t *testing.T) {
	m, s := newCreateManager(t)
	a, err := m.Create(context.Background(), createActor, CreateInput{Name: "Plain", Description: "d"})
	if err != nil {
		t.Fatal(err)
	}
	got, found, err := s.AgentBySlug(a.Slug)
	if err != nil || !found {
		t.Fatalf("not stored: %v %v", found, err)
	}
	if got.Prompt != "" || got.Metadata != "" || got.Tools != "[]" || got.AvatarColor != "" || got.AvatarType != "minion" {
		t.Fatalf("a bare create must not invent anything: %+v", got)
	}
}

func TestCreateStoresPromptTechToolsAndColour(t *testing.T) {
	m, s := newCreateManager(t)
	_, err := m.Create(context.Background(), createActor, CreateInput{
		Name: "Release notes writer", Prompt: "You write release notes.",
		Tech: []string{"go", "docs", "go"}, Tools: []string{"grep", "read", "read", "glob"}, AvatarColor: "pink",
	})
	if err != nil {
		t.Fatal(err)
	}
	got, _, _ := s.AgentBySlug("release-notes-writer")
	if got.Prompt != "You write release notes." || got.AvatarColor != "pink" {
		t.Fatalf("prompt/colour not stored: %+v", got)
	}
	if got.Tools != `["glob","grep","read"]` {
		t.Fatalf("tools must be stored clean and sorted, got %s", got.Tools)
	}
	if !strings.Contains(got.Metadata, `"tech":["docs","go"]`) {
		t.Fatalf("tech must be stored normalized in the metadata blob, got %s", got.Metadata)
	}
}

func TestCreateRefusesWhatNobodyCompiledAndWritesNothing(t *testing.T) {
	m, s := newCreateManager(t)
	for name, in := range map[string]CreateInput{
		"unknown tool": {Name: "A", Tools: []string{"read", "wrtie"}},
		"unknown tech": {Name: "B", Tech: []string{"cobol"}},
		"bad colour":   {Name: "C", AvatarColor: `red;background:url(x)`},
		"unnameable":   {Name: "!!!"},
	} {
		if _, err := m.Create(context.Background(), createActor, in); err == nil || !strings.Contains(err.Error(), "refused") {
			t.Errorf("%s: want a refusal, got %v", name, err)
		}
	}
	rows, err := s.AgentsAll()
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.Slug == "a" || r.Slug == "b" || r.Slug == "c" {
			t.Fatalf("a refused create left a row behind: %s", r.Slug)
		}
	}
}

// Edit: a caller that sends no tools leaves the allow-list; one that names the list sets it, even
// to nothing; one that names a tool the registry lacks is refused by name.
func TestEditToolsKeepsSetsClearsAndRefuses(t *testing.T) {
	m, s := newCreateManager(t)
	a, err := m.Create(context.Background(), createActor, CreateInput{Name: "Tooled", Tools: []string{"read", "list"}})
	if err != nil {
		t.Fatal(err)
	}
	edit := func(tools string) error {
		cur, _, _ := s.AgentBySlug(a.Slug)
		cur.Tools = tools
		return m.Edit(context.Background(), createActor, a.Slug, cur, "2026-11-11T10:00:00Z")
	}
	read := func() string { got, _, _ := s.AgentBySlug(a.Slug); return got.Tools }

	if err := edit(""); err != nil || read() != `["list","read"]` {
		t.Fatalf("no tools sent must leave them: %v %s", err, read())
	}
	if err := edit(`["bash","read"]`); err != nil || read() != `["bash","read"]` {
		t.Fatalf("a named list must be set: %v %s", err, read())
	}
	if err := edit(`[]`); err != nil || read() != `[]` {
		t.Fatalf("an empty list is a choice and must clear: %v %s", err, read())
	}
	if err := edit(`["read","nope"]`); err == nil || !strings.Contains(err.Error(), "nope") || read() != `[]` {
		t.Fatalf("an unknown tool must be refused by name and change nothing: %v %s", err, read())
	}
	if err := edit(`not json`); err == nil || !strings.Contains(err.Error(), "JSON list") {
		t.Fatalf("a malformed list must be refused: %v", err)
	}
}

// A copy of a shipped agent keeps the tools its source holds — the copy is the editable twin.
func TestDuplicateCarriesTheTools(t *testing.T) {
	m, s := newCreateManager(t)
	src, err := m.Create(context.Background(), createActor, CreateInput{Name: "Source", Tools: []string{"read", "grep"}})
	if err != nil {
		t.Fatal(err)
	}
	cp, err := m.Duplicate(context.Background(), createActor, src.Slug, "Copy")
	if err != nil {
		t.Fatal(err)
	}
	got, _, _ := s.AgentBySlug(cp.Slug)
	if got.Tools != `["grep","read"]` {
		t.Fatalf("the copy lost the tools: %s", got.Tools)
	}
}

// The person's yes, for one import, admits an unsigned card with the standing setting off: the row
// is marked unsigned, marked as imported, and a second import without the yes is refused again.
func TestAcceptUnsignedAdmitsOneImportMarkedUnsignedAndImported(t *testing.T) {
	s := openAgentStore(t)
	mgr := NewManager(s, fakeVerifier(agentcard.CardStateUnsigned), nil, boolOverride(false))
	card := writeCard(t, store.Agent{Slug: "groot", Name: "Groot", Version: "1"})

	if _, _, err := mgr.Import(context.Background(), testPerson, card); err == nil || !strings.Contains(err.Error(), "unsigned imports are refused by default") {
		t.Fatalf("without the yes the card is refused: %v", err)
	}
	a, res, err := mgr.Import(context.Background(), testPerson, card, ImportOptions{AcceptUnsigned: true})
	if err != nil || res != store.ImportResultCreated {
		t.Fatalf("with the yes: %v %v", res, err)
	}
	got, _, _ := s.AgentBySlug(a.Slug)
	if got.Signed || got.Origin != "import" {
		t.Fatalf("the row must be unsigned and imported: signed=%v origin=%s", got.Signed, got.Origin)
	}
	// The yes was for that import: a card for another agent is refused again.
	other := writeCard(t, store.Agent{Slug: "rocket", Name: "Rocket", Version: "1"})
	if _, _, err := mgr.Import(context.Background(), testPerson, other); err == nil {
		t.Fatal("one yes must not become a standing permission")
	}
}

// A copy is the person's own agent even when its source was first-party.
func TestDuplicateOfAShippedAgentIsOriginUser(t *testing.T) {
	m, s := newCreateManager(t)
	if err := s.CreateAgentAudited(context.Background(), createActor, store.Agent{Slug: "shipped", Name: "Shipped", Version: "1", Internal: true, Origin: "system"}, "2026-11-11T09:00:00Z"); err != nil {
		t.Fatal(err)
	}
	cp, err := m.Duplicate(context.Background(), createActor, "shipped", "Mine")
	if err != nil {
		t.Fatal(err)
	}
	got, _, _ := s.AgentBySlug(cp.Slug)
	if got.Origin != "user" || got.Internal {
		t.Fatalf("a copy must be the person's own: origin=%s internal=%v", got.Origin, got.Internal)
	}
}

// Which minion an agent wears is kept in its metadata beside the tech tags, validated at create, and
// an edit that does not touch it leaves it alone.
func TestCreateStoresTheMinionAndRefusesOneThatDoesNotExist(t *testing.T) {
	m, s := newCreateManager(t)
	if _, err := m.Create(context.Background(), createActor, CreateInput{Name: "Bopper", AvatarMinion: "bop", Tech: []string{"go"}}); err != nil {
		t.Fatal(err)
	}
	got, _, _ := s.AgentBySlug("bopper")
	if !strings.Contains(got.Metadata, `"minion":"bop"`) || !strings.Contains(got.Metadata, `"tech":["go"]`) {
		t.Fatalf("the minion and the tech must both ride the metadata: %s", got.Metadata)
	}
	if _, err := m.Create(context.Background(), createActor, CreateInput{Name: "Nope", AvatarMinion: "kevin"}); err == nil || !strings.Contains(err.Error(), "kevin") {
		t.Fatalf("an unknown minion is refused by name: %v", err)
	}
	if _, found, _ := s.AgentBySlug("nope"); found {
		t.Fatal("a refused create left a row")
	}
	// A bare create has none: the agent is drawn from its name.
	if _, err := m.Create(context.Background(), createActor, CreateInput{Name: "Plain Two"}); err != nil {
		t.Fatal(err)
	}
	if plain, _, _ := s.AgentBySlug("plain-two"); strings.Contains(plain.Metadata, "minion") {
		t.Fatalf("no minion chosen means none stored: %s", plain.Metadata)
	}
}
