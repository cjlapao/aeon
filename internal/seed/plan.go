package seed

import "sort"

// LedgerState is the ledger row for one record, or StateAbsent when the
// record has no ledger entry yet. It is the engine's memory of what was
// applied and at which version (SD3); status reads it.
type LedgerState struct {
	// Version is the applied_version; 0 when State is StateAbsent.
	Version int
	// Hash is the applied_hash (the canonical content hash at apply time);
	// "" when State is StateAbsent.
	Hash string
	// State is the ledger's per-record state, or StateAbsent for no row.
	State State
}

// LiveState is the live row's fingerprint: its content hash, whether the row
// is present, and — for a present row with no ledger row — whether the
// applier would adopt it (SD9). The engine populates it by calling the
// applier's Fingerprint and its adoptability check; Plan itself is pure.
type LiveState struct {
	// Hash is the live row's canonical content hash; "" when absent.
	Hash string
	// Present is whether the live row exists.
	Present bool
	// Adoptable is the applier's call for a present row with no ledger
	// entry: is it an adoptable stub (SD9) or a conflict? Only consulted
	// when Present is true and there is no ledger row.
	Adoptable bool
}

// Decision is what applying a set would do for one record (SD6): the pure,
// total output of Plan. A refusal (a diverge, a removed, a conflict) is a
// Decision with Op=skip and a reason — EXCEPT a downgrade, which is a
// Refusal on the result that fails the whole apply (T3), because a silent
// downgrade is a corruption, not a skip.
type Decision struct {
	Key         string
	Op          Op
	State       State
	FromVersion int
	ToVersion   int
	Reason      string
}

// Refusal is a plan-level refusal (the only one is a downgrade): it fails
// the whole apply before any write (SD6, SD10), named.
type Refusal struct {
	Key    string
	Reason string
}

// PlanResult is Plan's output: the per-record decisions (sorted by key) and
// any plan-level refusal.
type PlanResult struct {
	Decisions []Decision
	Refusal   Refusal
}

// Plan is the pure, total decision function (SD6): given the ledger and the
// live fingerprints, what applying the set would do for every record — and
// for every ledger row the set no longer carries (a retire). It reads no
// clock, store or disk; the engine feeds it the states. It never errors:
// a refusal is a skip with a reason, except a downgrade, which is a
// Refusal (the one thing that fails the whole apply, loudly). Decisions
// are sorted by key, so status and plan are deterministic.
// Plan is the pure, total decision function (SD6): given the ledger and the
// live fingerprints, what applying the set would do for every record — and
// for every ledger row the set no longer carries (a retire). It reads no
// clock, store or disk; the engine feeds it the states. It never errors:
// a refusal is a skip with a reason, except a downgrade, which is a
// Refusal (the one thing that fails the whole apply, loudly). Decisions
// are sorted by key, so status and plan are deterministic.
//
// It is one read of the set's source (S72): Set.Records is a function, not a
// cached list, so every caller that walks records per record takes the slice
// from here or from a setSnapshot, never by re-invoking the source.
func Plan(set Set, ledgers map[string]LedgerState, live map[string]LiveState) PlanResult {
	return planRecords(set.Records(), ledgers, live)
}

// planRecords is Plan over records already read. The keys of the slice are
// indexed once, so the retire pass below costs one map lookup per ledger row
// rather than one source read per ledger row.
func planRecords(records []Record, ledgers map[string]LedgerState, live map[string]LiveState) PlanResult {
	result := PlanResult{}
	keys := make(map[string]bool, len(records))
	for _, r := range records {
		keys[r.Key] = true
		d := planRecord(r, ledgers[r.Key], live[r.Key])
		if d.Refused() {
			result.Refusal = Refusal{Key: r.Key, Reason: d.Reason}
			// A downgrade fails the whole apply: no decisions are applied.
			return result
		}
		result.Decisions = append(result.Decisions, d)
	}
	// Retire a ledger row whose record left the code: it is in the ledger
	// but not in the set's records. The row is kept, never deleted (SD6).
	for key, ls := range ledgers {
		if keys[key] {
			continue
		}
		if ls.State == StateRetired {
			continue // already retired: a no-op, no decision
		}
		result.Decisions = append(result.Decisions, Decision{
			Key:         key,
			Op:          OpRetire,
			State:       StateRetired,
			FromVersion: ls.Version,
			Reason:      "retired — the record left the code (the row is kept, never auto-deleted)",
		})
	}
	sort.Slice(result.Decisions, func(i, j int) bool {
		return result.Decisions[i].Key < result.Decisions[j].Key
	})
	return result
}

// planRecord decides one record from its ledger and live states — the
// decision table in T1, one case per row. A downgrade is a Decision with
// a Refused() marker (the engine turns it into a plan Refusal).
func planRecord(r Record, ls LedgerState, live LiveState) Decision {
	// A missing ledger entry is an absent state, not the zero value: the
	// map hands back State="" for a key with no row, and "" is not
	// StateAbsent, so normalise it here or the whole plan falls through to
	// the default skip.
	if ls.State == "" {
		ls.State = StateAbsent
	}
	switch ls.State {
	case StateAbsent:
		return planAbsent(r, live)
	case StateApplied:
		return planApplied(r, ls, live)
	case StateRemoved:
		return Decision{
			Key:    r.Key,
			Op:     OpSkip,
			State:  StateRemoved,
			Reason: "removed by an operator; seed.restore re-applies",
		}
	case StateDiverged:
		return Decision{
			Key:    r.Key,
			Op:     OpSkip,
			State:  StateDiverged,
			Reason: "diverged — live content differs from v" + itoa(ls.Version) + " applied; seed.restore re-applies",
		}
	case StateRetired:
		// The record reappeared in the code: re-adopt it (op=apply).
		return Decision{
			Key:         r.Key,
			Op:          OpApply,
			State:       StateApplied,
			FromVersion: 0,
			ToVersion:   r.Version,
			Reason:      "re-adopted — the record returned to the code after a retire",
		}
	case StateFailed:
		return Decision{
			Key:         r.Key,
			Op:          OpApply,
			State:       StateApplied,
			FromVersion: ls.Version,
			ToVersion:   r.Version,
			Reason:      "retry a failed apply",
		}
	default:
		return Decision{Key: r.Key, Op: OpSkip, State: StateAbsent, Reason: "no ledger state"}
	}
}

func planAbsent(r Record, live LiveState) Decision {
	if !live.Present {
		// No ledger, no row: create.
		return Decision{
			Key:         r.Key,
			Op:          OpApply,
			State:       StateApplied,
			FromVersion: 0,
			ToVersion:   r.Version,
			Reason:      "apply — a new record",
		}
	}
	// No ledger, but a live row: adopt if the applier says adoptable, else
	// a conflict (never overwritten — S43's slug-collision rule, SD9).
	if live.Adoptable {
		return Decision{
			Key:         r.Key,
			Op:          OpAdopt,
			State:       StateApplied,
			FromVersion: 0,
			ToVersion:   r.Version,
			Reason:      "adopt — a S43 stub row is filled in place (id and created_at kept)",
		}
	}
	return Decision{
		Key:    r.Key,
		Op:     OpSkip,
		State:  StateAbsent,
		Reason: "conflict — a live row holds this key but is not an adoptable stub (never overwritten)",
	}
}

func planApplied(r Record, ls LedgerState, live LiveState) Decision {
	if !live.Present {
		// Applied in the ledger, but the row is gone: an operator removed
		// it. Report removed, skip (seed.restore re-applies).
		return Decision{
			Key:    r.Key,
			Op:     OpSkip,
			State:  StateRemoved,
			Reason: "removed by an operator; seed.restore re-applies",
		}
	}
	// The row is present. If its content differs from the applied hash, it
	// is diverged — never overwritten (SD6), regardless of version.
	if live.Hash != ls.Hash {
		return Decision{
			Key:         r.Key,
			Op:          OpSkip,
			State:       StateDiverged,
			FromVersion: ls.Version,
			ToVersion:   r.Version,
			Reason:      "diverged — live content differs from v" + itoa(ls.Version) + " applied; code is v" + itoa(r.Version) + "; seed.restore re-applies",
		}
	}
	// The row is unmodified (its content hash equals the applied hash).
	switch {
	case r.Version > ls.Version:
		return Decision{
			Key:         r.Key,
			Op:          OpUpdate,
			State:       StateApplied,
			FromVersion: ls.Version,
			ToVersion:   r.Version,
			Reason:      "update — a higher version over an unmodified row",
		}
	case r.Version == ls.Version:
		return Decision{
			Key:         r.Key,
			Op:          OpSkip,
			State:       StateApplied,
			FromVersion: ls.Version,
			ToVersion:   r.Version,
			Reason:      "up to date",
		}
	default: // r.Version < ls.Version: a downgrade is a refusal, never silent.
		return Decision{
			Key:         r.Key,
			Op:          OpSkip,
			State:       StateApplied,
			FromVersion: ls.Version,
			ToVersion:   r.Version,
			Reason:      "a lower version than applied — a downgrade is never silent",
		}
	}
}

// Refused marks a downgrade decision (the one that fails the whole apply).
// It is set on the skip-with-reason that names the downgrade.
func (d Decision) Refused() bool { return d.Op == OpSkip && d.Reason == "a lower version than applied — a downgrade is never silent" }

// PlanRestore is the pure decision for a seed.restore of one record (SD6):
// it re-applies the code version over the live row, only for a record in a
// removable state (removed, diverged, retired). Restoring an up-to-date or
// already-applied record is a no-op with a reason.
func PlanRestore(set Set, key string, ledger LedgerState, live LiveState) Decision {
	r, inSet := recordByKey(set, key)
	return planRestoreRecord(key, r, inSet, ledger, live)
}

// planRestoreRecord is PlanRestore over a record the caller already read, so
// a restore of one record costs one source read, not one per lookup
// (snapshot.go). inSet is whether the key was found; r is meaningless when it
// is not, exactly as recordByKey handed it back.
func planRestoreRecord(key string, r Record, inSet bool, ledger LedgerState, live LiveState) Decision {
	if !inSet {
		return Decision{Key: key, Op: OpSkip, Reason: "restore refused — the record is not in the set"}
	}
	switch ledger.State {
	case StateRemoved, StateDiverged, StateRetired:
		return Decision{
			Key:         key,
			Op:          OpRestore,
			State:       StateApplied,
			FromVersion: ledger.Version,
			ToVersion:   r.Version,
			Reason:      "restore — re-applied v" + itoa(r.Version) + " over the " + string(ledger.State) + " row",
		}
	default:
		return Decision{Key: key, Op: OpSkip, Reason: "restore refused — the record is up to date; nothing to restore"}
	}
}

func recordByKey(set Set, key string) (Record, bool) {
	for _, r := range set.Records() {
		if r.Key == key {
			return r, true
		}
	}
	return Record{}, false
}

// itoa formats an int for the decision reason text (SD6 names the versions
// it compares). Kept local so the package stays stdlib-only and the reason
// strings are stable.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
