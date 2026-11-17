package backup

// FreeState is the free-space answer's three states (BK17): the platform
// says there is plenty or too little, or it cannot say at all. "Cannot say"
// is a state a run records, never a silent pass — a backup taken on a disk
// whose fullness nobody measured is one that may have failed halfway, and
// the ledger must say so.
type FreeState string

const (
	// FreeOK: the platform measured the free bytes and they clear the
	// threshold the caller supplies.
	FreeOK FreeState = "ok"
	// FreeLow: the platform measured the free bytes and they do not.
	FreeLow FreeState = "low"
	// FreeUnknown: the platform cannot measure (a Windows build, an
	// unusual filesystem, a permission the query needs and lacks).
	FreeUnknown FreeState = "unknown"
)

// spaceThresholdFactor is BK4's multiplier: the snapshot is a full copy of
// the database plus its WAL, and the publish needs headroom beside it, so
// the run refuses when the free space is under 1.2 × (db + wal).
const spaceThresholdFactor = 1.2

// freeSpace is the platform query, a variable so a test can stand in for
// the filesystem (the three-state test does exactly that). Production code
// never reassigns it.
var freeSpace = queryFreeSpace

// classifyFreeSpace applies BK4's threshold to the platform's answer. A
// platform that already said low or unknown passes through untouched — the
// threshold only decides between ok and low on the bytes the platform did
// manage to measure. need is db + wal in bytes.
func classifyFreeSpace(dir string, need int64) (uint64, FreeState) {
	bytes, state := freeSpace(dir)
	switch state {
	case FreeOK:
		if int64(bytes) < int64(float64(need)*spaceThresholdFactor) {
			return bytes, FreeLow
		}
		return bytes, FreeOK
	default:
		return bytes, state
	}
}
