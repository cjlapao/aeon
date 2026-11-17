package continuous

import "time"

// Clock is the seam CN19's tick uses to name "now", and this file is the
// package's ONE honest home of the wall clock: SystemClock.Now below is
// the only time.Now in the package (TestNoTimeNowOutsideTheSystemClock
// counts it, the trigger's S20 precedent), and even that exists only so
// production wiring has something to pass — Plan and Preview take `now`
// as SNAPSHOT DATA and never call any clock. A clock added for tests
// later is a clock with two code paths, and the unticked one runs at 3am.
type Clock interface {
	Now() time.Time
}

// SystemClock is the production wiring.
type SystemClock struct{}

// Now is the package's only read of the wall.
func (SystemClock) Now() time.Time { return time.Now() }
