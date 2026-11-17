// gate_seams_test.go — S57 T8 (V13 side, VG18): the three gate-plane seam
// stubs the exact-set handshake test wires, and the test that pins the
// other side of the conditional registration: with the seams nil, the
// twenty-two names are NOT registered at all. A client gets JSON-RPC's
// own -32601 — the fact that the build did not link the door — never a
// refusal (an absent door is a fact about the build while a refusal is a
// fact about a decision; the two must never be confused, the
// TestBackupMethodsAbsentWhenServiceNil precedent).
//
// The stubs are zero-valued: their answers are never read by the
// handshake test (it counts names, not payloads); a stub that returned
// anything interesting would be a promise the exact-set test does not
// keep.

package protocol

import (
	"context"
	"errors"
	"testing"
)

// --- the verify stub ------------------------------------------------------------

type testVerifyService struct{}

func (testVerifyService) List(context.Context, VerifyListQuery) (VerifyListResult, error) {
	return VerifyListResult{}, nil
}

func (testVerifyService) Show(context.Context, string) (VerifyShowResult, error) {
	return VerifyShowResult{}, nil
}

func (testVerifyService) Runs(context.Context, VerifyRunsQuery) (VerifyRunsResult, error) {
	return VerifyRunsResult{}, nil
}

func (testVerifyService) RunShow(context.Context, string) (VerifyRunShowResult, error) {
	return VerifyRunShowResult{}, nil
}

func (testVerifyService) Vocabulary(context.Context) (VerifyVocabularyResult, error) {
	return VerifyVocabularyResult{}, nil
}

func (testVerifyService) Templates(context.Context) (VerifyTemplatesResult, error) {
	return VerifyTemplatesResult{}, nil
}

func (testVerifyService) Add(context.Context, Actor, VerifyAddInput) (VerifyWriteResult, *VerifyRefusal, error) {
	return VerifyWriteResult{}, nil, nil
}

func (testVerifyService) Update(context.Context, Actor, VerifyUpdateInput) (VerifyWriteResult, *VerifyRefusal, error) {
	return VerifyWriteResult{}, nil, nil
}

func (testVerifyService) Archive(context.Context, Actor, string) (VerifyWriteResult, *VerifyRefusal, error) {
	return VerifyWriteResult{}, nil, nil
}

func (testVerifyService) Restore(context.Context, Actor, string) (VerifyWriteResult, *VerifyRefusal, error) {
	return VerifyWriteResult{}, nil, nil
}

func (testVerifyService) ApplyTemplate(context.Context, Actor, string, []string) (VerifyApplyResult, *VerifyRefusal, error) {
	return VerifyApplyResult{}, nil, nil
}

func (testVerifyService) Try(context.Context, Actor, string, string) (VerifyTryResult, *VerifyRefusal, error) {
	return VerifyTryResult{}, nil, nil
}

// --- the merge-rule stub ------------------------------------------------------------

type testMergeRuleService struct{}

func (testMergeRuleService) Get(context.Context, string, bool) (MergeRuleGetResult, error) {
	return MergeRuleGetResult{}, nil
}

func (testMergeRuleService) Preview(context.Context, string, int) (MergeRulePreviewResult, error) {
	return MergeRulePreviewResult{}, nil
}

func (testMergeRuleService) Changes(context.Context, string, int) (MergeRuleChangesResult, error) {
	return MergeRuleChangesResult{}, nil
}

func (testMergeRuleService) Vocabulary(context.Context) (MergeRuleVocabularyResult, error) {
	return MergeRuleVocabularyResult{}, nil
}

func (testMergeRuleService) Set(context.Context, Actor, MergeRuleSetInput) (MergeRuleSetResult, *MergeRuleRefusal, error) {
	return MergeRuleSetResult{}, nil, nil
}

func (testMergeRuleService) Clear(context.Context, Actor, string) (MergeRuleSetResult, *MergeRuleRefusal, error) {
	return MergeRuleSetResult{}, nil, nil
}

func (testMergeRuleService) Events(context.Context, MergeEventsQuery) (MergeEventsResult, error) {
	return MergeEventsResult{}, nil
}

func (testMergeRuleService) Watches(context.Context, MergeWatchesQuery) (MergeWatchesResult, error) {
	return MergeWatchesResult{}, nil
}

// --- the DoD stub -----------------------------------------------------------------------

type testDoDService struct{}

func (testDoDService) List(context.Context, string) (DoDListResult, error) {
	return DoDListResult{}, nil
}

func (testDoDService) Mark(context.Context, Actor, DoDMarkInput) (DoDWriteResult, *DoDRefusal, error) {
	return DoDWriteResult{}, nil, nil
}

// --- the V13 side: absent, not refusing ---------------------------------------------------

// TestMethodsAbsentWhenServiceNil is the V13 side of the gate plane:
// with the three seams nil, the twenty-two names are not registered —
// the dispatcher answers -32601 for every one, the build-fact, never a
// family refusal (the refusal code is a decision about a request the
// door understood; an absent door understood nothing).
func TestMethodsAbsentWhenServiceNil(t *testing.T) {
	tbl := newTestTable(t)
	initialized(t, tbl, "c-gates-absent", PROTOCOL_VERSION)
	for _, m := range []MethodName{
		MethodVerifyList, MethodVerifyShow, MethodVerifyRuns, MethodVerifyRunShow,
		MethodVerifyVocabulary, MethodVerifyTemplates,
		MethodVerifyAdd, MethodVerifyUpdate, MethodVerifyArchive, MethodVerifyRestore,
		MethodVerifyApplyTemplate, MethodVerifyTry,
		MethodMergeRuleGet, MethodMergeRulePreview, MethodMergeRuleChanges,
		MethodMergeRuleVocabulary, MethodMergeRuleSet, MethodMergeRuleClear,
		MethodMergeEvents, MethodMergeWatches,
		MethodDoDList, MethodDoDMark,
	} {
		_, err := tbl.Dispatch(context.Background(), "c-gates-absent", m, nil)
		if err == nil {
			t.Fatalf("%s succeeded on a table whose gate seams are nil — V13 is a registration, not a refusal", m)
		}
		var unknown ErrUnknownMethod
		if !errors.As(err, &unknown) {
			t.Fatalf("%s: absent wants ErrUnknownMethod (the -32601 the transport answers), got %T: %v", m, err, err)
		}
	}
}
