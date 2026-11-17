package store

import (
	"context"
	"testing"
)

// A seeded agent is a yellow minion at the seed record's version; a re-seed
// recolours a row still wearing the old default (blue) and leaves a colour a
// person chose.
func TestSeededAgentsAreYellowMinionsAtTheSeedVersion(t *testing.T) {
	st := ownerStore(t)
	ctx := context.Background()
	actor := AgentActor{Kind: "system", ID: "system:seed-engine"}
	tx, err := st.w.Begin()
	if err != nil {
		t.Fatal(err)
	}
	c := AgentContent{Slug: "pip-agent", Name: "pip-agent", Prompt: "p", Tools: "[]", Handoff: "{}", Origin: "system", Metadata: "{}", Version: "2"}
	if err := st.SeedAgentInsert(ctx, tx, c, actor, "2026-11-08T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	read := func() (version, color string) {
		if err := st.r.QueryRow(`SELECT version, avatar_color FROM agent WHERE slug = 'pip-agent'`).Scan(&version, &color); err != nil {
			t.Fatal(err)
		}
		return
	}
	if v, col := read(); v != "2" || col != "yellow" {
		t.Fatalf("inserted at version %q colour %q", v, col)
	}

	update := func(version string) {
		tx, err := st.w.Begin()
		if err != nil {
			t.Fatal(err)
		}
		c.Version = version
		if err := st.SeedAgentUpdate(ctx, tx, c, actor, "2026-11-08T00:00:01Z"); err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := st.w.Exec(`UPDATE agent SET avatar_color = 'blue' WHERE slug = 'pip-agent'`); err != nil {
		t.Fatal(err)
	}
	update("3")
	if v, col := read(); v != "3" || col != "yellow" {
		t.Fatalf("a blue seed default is recoloured and the version moves: %q %q", v, col)
	}
	if _, err := st.w.Exec(`UPDATE agent SET avatar_color = 'purple' WHERE slug = 'pip-agent'`); err != nil {
		t.Fatal(err)
	}
	update("4")
	if _, col := read(); col != "purple" {
		t.Fatalf("a colour a person chose stays: %q", col)
	}
}
