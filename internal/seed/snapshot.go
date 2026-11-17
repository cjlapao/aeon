package seed

// snapshot.go — one read of a set's source, indexed by key.
//
// Set.Records is a function, not a list, and it is deliberately not cached
// (seed.go): a domain's source rebuilds and canonicalises every record on
// each call. For the agents set that is a parse of every embedded body plus a
// canonical marshal — measured on this host at 69 ms for its 33 records, and
// 8.3 ms per call for the 137-record tool catalog.
//
// That cost is invisible when a set's records are read once and visible
// immediately when they are read once per record: an operation that resolves
// a key per record then costs N source reads, and the whole-set status door
// showed exactly that (S72, 2026-11-08: `seed.status` over the 15 registered
// sets took 6.4 s cold and 1.5 s warm, with 4.0 s in agents.firstParty and
// 2.0 s in tools.core — while the sets whose source is a static slice cost
// 0.02 s for more records). The engine's loops were correct; they were just
// paying a full source rebuild for every record they touched.
//
// A setSnapshot is the seam that fixes it: the source is read ONCE per
// operation, and every per-record lookup in that operation goes through the
// index. It is a snapshot in the strong sense — the engine plans, reports and
// writes from the same bytes, so status, plan and apply cannot disagree about
// what the code says halfway through an operation.
type setSnapshot struct {
	// set is the set the snapshot was taken of, kept so an operation can
	// report its id and version beside the records without holding two
	// values that could disagree.
	set Set
	// records is the one read of the source, in the source's order.
	records []Record
	// index maps a record key to its position in records.
	index map[string]int
}

// newSetSnapshot reads a set's source once and indexes it by key. A duplicate
// key keeps the first record: the registry refuses a set whose keys collide
// (registry.go's rule on records), so the first is the one every earlier
// linear scan would have found.
func newSetSnapshot(s Set) setSnapshot {
	recs := s.Records()
	sn := setSnapshot{set: s, records: recs, index: make(map[string]int, len(recs))}
	for i, r := range recs {
		if _, seen := sn.index[r.Key]; !seen {
			sn.index[r.Key] = i
		}
	}
	return sn
}

// version is the available version of one key — 0 when the key is not in the
// set, which is what a ledger row whose record left the code reports.
func (sn setSnapshot) version(key string) int {
	i, ok := sn.index[key]
	if !ok {
		return 0
	}
	return sn.records[i].Version
}

// record is the record for one key, or a bare key-only Record when the set
// does not carry it — the same value the linear scan handed back, so an
// applier handed a missing key sees exactly what it saw before.
func (sn setSnapshot) record(key string) Record {
	r, _ := sn.lookup(key)
	return r
}

// lookup is the indexed form of the old linear scan: the record and whether
// the set carries the key.
func (sn setSnapshot) lookup(key string) (Record, bool) {
	i, ok := sn.index[key]
	if !ok {
		return Record{Key: key}, false
	}
	return sn.records[i], true
}

// id is the set's own id, carried so an operation reads one value.
func (sn setSnapshot) id() string { return sn.set.ID }
