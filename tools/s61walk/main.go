// Command s61walk is the S61 T12 demo's loop-seat half: what the intake
// AGENT produces inside a live loop (the template seed, the intake_plan
// proposal, the charter_propose ask) is planted here, through the STORE's
// own registered methods, so the PERSON's half — every refusal, preview,
// commit and census in the walk — runs unchanged over the real CLI and the
// real daemon. The agent-loop-over-a-provider half cannot run in this
// session (no provider credential is present, and the real forge is out of
// scope by the brief); those steps are quoted to their named tests, as the
// prior slices' walks do. Every line this prints is PLANTED-marked.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/cjlapao/aeon/internal/desk"
	"github.com/cjlapao/aeon/internal/store"
)

func main() {
	dir := flag.String("dir", "", "state dir holding the walk's store")
	do := flag.String("do", "", "template | seed | charter-propose | census")
	intake := flag.String("intake", "", "intake session id (seed)")
	root := flag.String("root", "", "root component id (charter-propose, census)")
	flag.Parse()
	if *dir == "" || *do == "" {
		fmt.Fprintln(os.Stderr, "s61walk: -dir and -do are required")
		os.Exit(2)
	}
	st, err := store.Open(store.Options{Dir: *dir})
	fatal("open", err)
	defer func() { _ = st.Close() }()
	act := store.Actor{Kind: "agent", ID: "intake-loop(planted)"}
	at := func() string { return time.Now().UTC().Format(time.RFC3339) }

	switch *do {
	case "template":
		err = retryBusy("save template", func() error {
			_, refusal, e := st.SaveTemplate(store.NewTemplate{
				Slug: "base", Title: "Base", Description: "the shipped floor",
				StackTags:  []string{"golang"},
				Files:      []store.TemplateFile{{Path: "README.md", Mode: 0o644, Content: "# {{.Name}}\n"}},
				SeedOrigin: "system",
			}, store.Actor{Kind: "user", ID: "walk-seed"})
			_ = refusal // first save in a fresh store; refusal shape is the template plane's, not walked here
			return e
		})
		fatal("template", err)
		fmt.Println("PLANTED template base")

	case "seed":
		// A valid plan for THREE components in a tree (one root, two
		// children), one epic, two goals with criteria, three items. The
		// daemon's validators read every byte of it at the plan door exactly
		// as they read the conformance arms' plans.
		plan := desk.IntakePlanProposal{
			IntakeID:    *intake,
			Charter:     json.RawMessage(`{"goal":"run the shop end to end","constraints":["the person owns the money"],"nonGoals":["no payments before the person approves one"]}`),
			ProjectName: "Shop",
			Components: []desk.IntakePlanComponent{
				{Name: "Shop", Slug: "shop", Template: "base", Role: "root"},
				{Name: "Shop Web", Slug: "shop-web", Template: "base", Role: "child"},
				{Name: "Shop Jobs", Slug: "shop-jobs", Template: "base", Role: "child"},
			},
			Epics: []desk.IntakePlanEpic{{Key: "E1", Title: "Launch", Objective: "the shop takes its first real order"}},
			Goals: []desk.IntakePlanGoal{
				{EpicKey: "E1", Title: "Catalog live", Type: "feature", ComponentSlugs: []string{"shop", "shop-web"},
					Criteria: []desk.IntakePlanCriterion{{Text: "a visitor lists products"}}},
				{EpicKey: "E1", Title: "Orders flow", Type: "feature", ComponentSlugs: []string{"shop-jobs"},
					Criteria: []desk.IntakePlanCriterion{{Text: "an order reaches the jobs queue"}}},
			},
			Items: []desk.IntakePlanItem{
				{ComponentSlug: "shop-web", EpicKey: "E1", Title: "Product list page", Priority: "unknown", Estimate: "3"},
				{ComponentSlug: "shop-web", EpicKey: "E1", Title: "Product page", Priority: "unknown", Estimate: "3"},
				{ComponentSlug: "shop-jobs", EpicKey: "E1", Title: "Order queue worker", Priority: "unknown", Estimate: "5-8"},
			},
			Reason: "The person described a shop; this is the plan the intake loop produced.",
		}
		payload, err := json.Marshal(plan)
		fatal("encode", err)
		var row store.Proposal
		err = retryBusy("plant intake_plan", func() error {
			var e error
			row, e = st.CreateProposal(string(desk.KindIntakePlan), string(payload), act, at())
			return e
		})
		fatal("seed", err)
		fmt.Printf("PLANTED intake_plan proposal id=%d (intakeId=%s)\n", row.ID, *intake)

	case "charter-propose":
		body := json.RawMessage(`{"goal":"run the shop end to end","constraints":["the person owns the money","the web tier never touches the ledger"],"nonGoals":["no payments before the person approves one"]}`)
		payload, err := json.Marshal(desk.CharterSaveProposal{RootID: *root, Body: body, Reason: "The loop proposed a second constraint after the person's review."})
		fatal("encode", err)
		var row store.Proposal
		err = retryBusy("plant charter_save", func() error {
			var e error
			row, e = st.CreateProposal(string(desk.KindCharterSave), string(payload), act, at())
			return e
		})
		fatal("charter-propose", err)
		fmt.Printf("PLANTED charter_save proposal id=%d (rootId=%s)\n", row.ID, *root)

	case "census":
		items, err := st.ListRoadmapItems("", store.RoadmapItemFilters{})
		fatal("items", err)
		var idea, unknown int
		for _, it := range items {
			if it.Status == "idea" {
				idea++
			}
			if it.PriorityState == "unknown" {
				unknown++
			}
		}
		fmt.Printf("census: items=%d (idea=%d, priority=unknown:%d)\n", len(items), idea, unknown)
		goals, err := st.ListGoals(store.GoalFilter{})
		fatal("goals", err)
		comps, err := st.ListComponents(store.ComponentFilter{})
		fatal("components", err)
		var inReview, others, roots, intakeSourced int
		for _, g := range goals {
			if g.SourceKind == "intake" {
				intakeSourced++
			}
			if g.ReviewState == "in_review" {
				inReview++
			} else {
				others++
			}
		}
		names := []string{}
		for _, c := range comps {
			if c.ParentID == "" {
				roots++
			}
			names = append(names, c.Name)
		}
		fmt.Printf("census: components=%d (roots=%d) %v · goals=%d (in_review=%d, other=%d, sourceKind=intake:%d)\n",
			len(comps), roots, names, len(goals), inReview, others, intakeSourced)

	default:
		fmt.Fprintln(os.Stderr, "s61walk: unknown -do")
		os.Exit(2)
	}
}

// retryBusy waits out a transient SQLITE_BUSY (a daemon checkpoint landing
// mid-write) for at most twelve seconds. A BUSY insert has not inserted —
// the refusal IS the insert — so a repeat cannot double-write; any other
// refusal returns at once. This is the store's single-writer design, walked
// honestly rather than papered over (internal/store/open.go's own comment
// is the law).
func retryBusy(what string, fn func() error) error {
	var err error
	for i := 0; i < 48; i++ {
		if err = fn(); err == nil {
			return nil
		}
		if !strings.Contains(err.Error(), "database is locked") {
			return fmt.Errorf("%s: %w", what, err)
		}
		time.Sleep(250 * time.Millisecond)
	}
	return err
}

func fatal(what string, err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "s61walk %s: %v\n", what, err)
		os.Exit(1)
	}
}
