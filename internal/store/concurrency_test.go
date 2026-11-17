package store

import (
	"strings"
	"sync"
	"testing"
	"time"
)

// T10's concurrency proof. DB15 arranges one writer and many readers; the only
// way to know it is right is sustained writes with reads happening throughout.

func TestSustainedWritesWithConcurrentReadsReportNoSQLiteBusy(t *testing.T) {
	// D11. `busy_timeout` papering over contention would show up here as
	// latency rather than as an error, which is why the read-latency bound is
	// asserted too.
	s := openTemp(t)
	sc := seedLoop(t, s, "hammer", "/repo")

	const writes = 400
	const readers = 4

	var wg sync.WaitGroup
	busy := make(chan string, 64)
	stop := make(chan struct{})

	// One writer, continuously.
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer close(stop)
		for i := 0; i < writes; i++ {
			if _, err := s.AppendEvent(NewEvent{ComponentRoot: sc.ComponentRoot, LoopSlug: sc.LoopSlug,
				Kind: "loop.progress", Payload: map[string]any{"i": i}}); err != nil {
				busy <- "writer: " + err.Error()
				return
			}
		}
	}()

	// Several readers, continuously, each timing its own worst read.
	worst := make([]time.Duration, readers)
	for r := 0; r < readers; r++ {
		wg.Add(1)
		go func(r int) {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				start := time.Now()
				if _, err := s.ReadEvents(0, EventFilter{ComponentRoot: sc.ComponentRoot}, 50); err != nil {
					busy <- "reader: " + err.Error()
					return
				}
				if took := time.Since(start); took > worst[r] {
					worst[r] = took
				}
			}
		}(r)
	}

	wg.Wait()
	close(busy)

	for msg := range busy {
		if strings.Contains(strings.ToUpper(msg), "BUSY") {
			t.Errorf("SQLITE_BUSY under sustained writes: %s", msg)
		} else {
			t.Errorf("a query failed under sustained writes: %s", msg)
		}
	}

	// readLatencyBound is a GUESS (O7). D11's QA case is expected to change it
	// rather than pass: what matters is that a read answers in a time a UI can
	// render, not that it answers in this particular time.
	const readLatencyBound = 2 * time.Second
	for r, took := range worst {
		if took > readLatencyBound {
			t.Errorf("reader %d's worst read took %v, over the stated bound of %v. A read on the "+
				"single-connection WRITER pool would look exactly like this", r, took, readLatencyBound)
		}
	}

	head, err := s.HeadSeq()
	if err != nil {
		t.Fatal(err)
	}
	if head != writes {
		t.Errorf("the sequence reached %d after %d writes", head, writes)
	}
}

func TestAConcurrentReaderNeverObservesAGapInTheEventSequence(t *testing.T) {
	// A reader must see a monotonically growing PREFIX — never a hole, which
	// would mean a torn read: seq N+1 visible while N is not. WAL guarantees a
	// consistent snapshot per read, and this asserts the guarantee holds
	// through Aeon's pools rather than assuming it.
	s := openTemp(t)
	sc := seedLoop(t, s, "prefix", "/repo")

	const writes = 300
	done := make(chan struct{})
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		defer close(done)
		for i := 0; i < writes; i++ {
			if _, err := s.AppendEvent(NewEvent{ComponentRoot: sc.ComponentRoot, Kind: "loop.progress"}); err != nil {
				return
			}
		}
	}()

	var mu sync.Mutex
	var holes []string

	for r := 0; r < 3; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// PER READER, not shared. Two readers legitimately see different
			// prefixes at the same moment — one may be two writes behind the
			// other — so comparing their high-water marks to each other finds
			// "backwards" reads that are nothing of the kind. (It did: the
			// first version of this test reported `194 after 196` and the store
			// was fine.) What must hold is that ONE reader never goes
			// backwards, and that every read is a gapless prefix.
			var highest int64
			for {
				select {
				case <-done:
					return
				default:
				}
				events, err := s.ReadEvents(0, EventFilter{ComponentRoot: sc.ComponentRoot}, 1000)
				if err != nil {
					return
				}
				for i, ev := range events {
					if ev.Seq != int64(i+1) {
						mu.Lock()
						holes = append(holes, "a read saw seq "+itoa(ev.Seq)+" at position "+
							itoa(int64(i+1))+" — a torn read")
						mu.Unlock()
						break
					}
				}
				if n := int64(len(events)); n > 0 {
					if events[n-1].Seq < highest {
						mu.Lock()
						holes = append(holes, "one reader went backwards: "+itoa(events[n-1].Seq)+
							" after "+itoa(highest))
						mu.Unlock()
					} else {
						highest = events[n-1].Seq
					}
				}
			}
		}()
	}

	wg.Wait()
	for _, h := range holes {
		t.Error(h)
	}
}

func TestTwoWritersSerialiseRatherThanFailing(t *testing.T) {
	// DB15's claim, under real contention: twenty goroutines writing at once,
	// and every write must land. The pool is at one connection, so nineteen of
	// them are waiting at any moment — which is the predictable serialisation
	// the design asked for rather than a busy_timeout retry loop.
	s := openTemp(t)

	const writers = 20
	const each = 10

	var wg sync.WaitGroup
	errs := make(chan error, writers*each)
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < each; i++ {
				if _, err := s.AppendEvent(NewEvent{Kind: "contention"}); err != nil {
					errs <- err
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("a write failed under contention: %v", err)
	}

	head, err := s.HeadSeq()
	if err != nil {
		t.Fatal(err)
	}
	if head != writers*each {
		t.Errorf("the sequence reached %d after %d concurrent writes; every write must land exactly once",
			head, writers*each)
	}
}

func TestTwoProcessesCanWriteTheSameFile(t *testing.T) {
	// `busy_timeout` and `_txlock=immediate` exist for this case: two PROCESSES
	// on one file, which is what the demo's `debug hammer` beside a CLI verb
	// actually is.
	//
	// Within one process the pool serialises; between processes SQLite's own
	// lock does, and a deferred transaction that upgrades to a write gets
	// SQLITE_BUSY IMMEDIATELY — busy_timeout does not cover a lock upgrade.
	// That is why the writer DSN takes the write lock at BEGIN.
	dir := t.TempDir()
	first := openAt(t, dir)
	second := openAt(t, dir)

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, s := range []*Store{first, second} {
		wg.Add(1)
		go func(s *Store) {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				if _, err := s.AppendEvent(NewEvent{Kind: "two.pools"}); err != nil {
					errs <- err
					return
				}
			}
		}(s)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if strings.Contains(strings.ToUpper(err.Error()), "BUSY") {
			t.Errorf("SQLITE_BUSY between two handles on one file: %v", err)
		} else {
			t.Errorf("a write failed between two handles: %v", err)
		}
	}

	head, err := first.HeadSeq()
	if err != nil {
		t.Fatal(err)
	}
	if head != 100 {
		t.Errorf("the sequence reached %d after 100 writes from two handles", head)
	}
}
