package store

import (
	"strings"
	"testing"
)

// T9's tests. Every refusal is proven BY ATTEMPT, and none is normalised into
// range.

func TestADocumentRoundTripsByFriendlyName(t *testing.T) {
	s := openTemp(t)
	sc := seedLoop(t, s, "docs", "/repo")

	res, err := s.WriteDocument(sc, "plan", "# Plan\n\nstep one.\n", "component-manager")
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK {
		t.Fatalf("refused: %s", res.Reason)
	}
	if res.Document.File != "plan.md" {
		t.Errorf("the friendly name resolved to %q, want plan.md", res.Document.File)
	}
	if res.Document.Rev != 1 {
		t.Errorf("the first revision is %d", res.Document.Rev)
	}

	back, err := s.ReadDocument(sc, "plan")
	if err != nil {
		t.Fatal(err)
	}
	if back.Content != "# Plan\n\nstep one.\n" {
		t.Errorf("content came back as %q", back.Content)
	}
	// And by its filename, which must reach the same row.
	byFile, err := s.ReadDocument(sc, "plan.md")
	if err != nil {
		t.Fatal(err)
	}
	if byFile.ID != back.ID {
		t.Error("`plan` and `plan.md` reached different rows")
	}
}

func TestADocumentRoundTripsByAnAllowedPath(t *testing.T) {
	s := openTemp(t)
	sc := seedLoop(t, s, "docs", "/repo")
	for _, name := range []string{"tasks/T1.md", "scratch/notes.txt", "events.jsonl"} {
		res, err := s.WriteDocument(sc, name, "content of "+name, "")
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !res.OK {
			t.Fatalf("%s was refused: %s", name, res.Reason)
		}
		back, err := s.ReadDocument(sc, name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if back.Content != "content of "+name {
			t.Errorf("%s came back as %q", name, back.Content)
		}
	}
}

func TestEveryDocumentPathRefusalIsProvenByAttempt(t *testing.T) {
	// D14. One case per reason, each attempting the write — not reading the
	// code. And NONE is normalised into range: a silent clamp turns an escape
	// attempt into a successful write somewhere unexpected, and it looks
	// exactly like the feature working.
	//
	// These names come from a MODEL, not from a person.
	s := openTemp(t)
	sc := seedLoop(t, s, "refusals", "/repo")

	cases := []struct {
		name string
		want DocumentRefusal
	}{
		{"/etc/passwd", RefusalAbsolute},
		{"../../../etc/passwd", RefusalTraversal},
		{"tasks/../../escape.md", RefusalTraversal},
		{"secrets/key.md", RefusalDirectory},
		{"tasks/deep/nested.md", RefusalDirectory},
		{"scratch/notes.exe", RefusalExtension},
		{"notes", RefusalExtension},
		{"", RefusalEmpty},
		{"   ", RefusalEmpty},
		{`C:\Windows\System32\drivers\etc\hosts`, RefusalAbsolute},
		{"plan\x00.md", RefusalTraversal},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := s.WriteDocument(sc, tc.name, "x", "")
			if err != nil {
				t.Fatalf("a refusal came back as an error rather than a result: %v", err)
			}
			if res.OK {
				t.Fatalf("%q was ACCEPTED, and stored as %q", tc.name, res.Document.File)
			}
			if res.Refusal != tc.want {
				t.Errorf("%q was refused as %q, want %q", tc.name, res.Refusal, tc.want)
			}
			if res.Reason == "" {
				t.Errorf("%q was refused with no reason", tc.name)
			}

			// NOT NORMALISED: nothing was written anywhere.
			docs, err := s.ListDocuments(sc)
			if err != nil {
				t.Fatal(err)
			}
			if len(docs) != 0 {
				t.Errorf("%q was refused but %d documents exist; a refusal must not become a write "+
					"somewhere else", tc.name, len(docs))
			}
		})
	}
}

func TestTheCheckRunsOnTheRawInputBeforeNormalisation(t *testing.T) {
	// A path cleaned first and checked second is a path whose escape has
	// already been resolved away: `tasks/../plan.md` would normalise to
	// `plan.md`, pass every check, and write — making the traversal attempt
	// succeed at a different target.
	res := ResolveDocumentName("tasks/../plan.md")
	if res.OK() {
		t.Fatalf("`tasks/../plan.md` resolved to %q; it must be refused, not cleaned", res.Name)
	}
	if res.Refusal != RefusalTraversal {
		t.Errorf("refused as %q, want traversal", res.Refusal)
	}

	// The same shape that WOULD be legal after cleaning.
	res = ResolveDocumentName("scratch/../tasks/T1.md")
	if res.OK() {
		t.Fatalf("a .. segment resolved to %q", res.Name)
	}
}

func TestAFriendlyNameWinsOverAnythingPathLike(t *testing.T) {
	// So `plan` can never be mistaken for a directory called plan.
	res := ResolveDocumentName("plan")
	if !res.OK() || res.Name != "plan.md" {
		t.Errorf("`plan` resolved to %+v", res)
	}
	// Case-insensitively, as in v1.
	res = ResolveDocumentName("PLAN")
	if !res.OK() || res.Name != "plan.md" {
		t.Errorf("`PLAN` resolved to %+v", res)
	}
}

func TestADocumentOverTheSizeLimitIsRefused(t *testing.T) {
	s := openTemp(t)
	sc := seedLoop(t, s, "big", "/repo")
	res, err := s.WriteDocument(sc, "plan", strings.Repeat("x", MaxDocumentBytes+1), "")
	if err != nil {
		t.Fatal(err)
	}
	if res.OK {
		t.Fatal("a document over the limit was accepted")
	}
	if res.Refusal != RefusalTooLarge {
		t.Errorf("refused as %q", res.Refusal)
	}
}

func TestDocumentsAreAppendOnlyByRevision(t *testing.T) {
	// `plan.md` is rewritten on a task split and again for QA-fix tasks, and
	// "what did the plan say before that" is usually the question worth
	// answering.
	s := openTemp(t)
	sc := seedLoop(t, s, "revisions", "/repo")

	for i, content := range []string{"first draft", "after the split", "after QA"} {
		res, err := s.WriteDocument(sc, "plan", content, "")
		if err != nil {
			t.Fatal(err)
		}
		if res.Document.Rev != i+1 {
			t.Errorf("write %d produced rev %d", i, res.Document.Rev)
		}
	}

	head, err := s.ReadDocument(sc, "plan")
	if err != nil {
		t.Fatal(err)
	}
	if head.Content != "after QA" {
		t.Errorf("the head revision is %q", head.Content)
	}

	revs, err := s.DocumentRevisions(sc, "plan")
	if err != nil {
		t.Fatal(err)
	}
	if len(revs) != 3 {
		t.Fatalf("got %d revisions, want 3", len(revs))
	}
	if revs[0].Rev != 3 {
		t.Errorf("revisions are not newest-first: %d came first", revs[0].Rev)
	}
}

func TestAWriteWhoseContentIsUnchangedIsDroppedRatherThanStoredTwice(t *testing.T) {
	// A third outcome, not a success with a side note: a caller that believed
	// it created a revision would show a history with a duplicate in it.
	s := openTemp(t)
	sc := seedLoop(t, s, "unchanged", "/repo")

	if _, err := s.WriteDocument(sc, "plan", "same content", ""); err != nil {
		t.Fatal(err)
	}
	res, err := s.WriteDocument(sc, "plan", "same content", "")
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK {
		t.Fatalf("an unchanged write was refused: %s", res.Reason)
	}
	if !res.Unchanged {
		t.Error("an unchanged write was not reported as unchanged")
	}
	if res.Document.Rev != 1 {
		t.Errorf("an unchanged write created rev %d", res.Document.Rev)
	}
	revs, _ := s.DocumentRevisions(sc, "plan")
	if len(revs) != 1 {
		t.Errorf("%d revisions exist after writing the same content twice", len(revs))
	}
}

func TestADocumentIsVerifiedAgainstItsStoredDigestOnRead(t *testing.T) {
	// The digest covers the RAW bytes, matching loop_archive_file so both
	// stores verify the same way. Corruption is reported where somebody would
	// notice, rather than returned as content.
	s := openTemp(t)
	sc := seedLoop(t, s, "corrupt", "/repo")
	if _, err := s.WriteDocument(sc, "plan", "the real content", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.w.Exec(`UPDATE loop_document SET sha256 = 'not-the-digest'`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReadDocument(sc, "plan"); err == nil {
		t.Fatal("a document whose content does not match its hash was returned")
	} else if !strings.Contains(err.Error(), "sha256") {
		t.Errorf("the error does not name the problem: %v", err)
	}
}

func TestDocumentsAreScopedToTheirLoop(t *testing.T) {
	s := openTemp(t)
	a := seedLoop(t, s, "refactor", "/repo-a")
	b := seedLoop(t, s, "refactor", "/repo-b")

	if _, err := s.WriteDocument(a, "plan", "A's plan", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.WriteDocument(b, "plan", "B's plan", ""); err != nil {
		t.Fatal(err)
	}

	fromA, err := s.ReadDocument(a, "plan")
	if err != nil {
		t.Fatal(err)
	}
	fromB, err := s.ReadDocument(b, "plan")
	if err != nil {
		t.Fatal(err)
	}
	if fromA.Content != "A's plan" || fromB.Content != "B's plan" {
		t.Errorf("documents crossed projects: A has %q, B has %q", fromA.Content, fromB.Content)
	}
	// And each list shows only its own, even though both revisions are rev 1.
	inA, err := s.ListDocuments(a)
	if err != nil {
		t.Fatal(err)
	}
	if len(inA) != 1 {
		t.Errorf("listing /repo-a's documents returned %d", len(inA))
	}
}

func TestListDocumentsReturnsOnlyHeadRevisions(t *testing.T) {
	s := openTemp(t)
	sc := seedLoop(t, s, "heads", "/repo")
	for i := 0; i < 3; i++ {
		if _, err := s.WriteDocument(sc, "plan", strings.Repeat("v", i+1), ""); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.WriteDocument(sc, "tasks/T1.md", "a task", ""); err != nil {
		t.Fatal(err)
	}

	docs, err := s.ListDocuments(sc)
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 2 {
		t.Fatalf("the listing returned %d documents, want one per file", len(docs))
	}
	for _, d := range docs {
		if d.File == "plan.md" && d.Rev != 3 {
			t.Errorf("plan.md listed at rev %d, want the head (3)", d.Rev)
		}
		if d.Content != "" {
			t.Error("the listing inflated content; a manifest that inflates every revision is one " +
				"nobody can list")
		}
	}
}

func TestAMissingDocumentReadsAsNotFound(t *testing.T) {
	s := openTemp(t)
	sc := seedLoop(t, s, "missing", "/repo")
	if _, err := s.ReadDocument(sc, "plan"); err == nil {
		t.Fatal("a document that was never written read back successfully")
	}
}
