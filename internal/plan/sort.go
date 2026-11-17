package plan

// The sort: a deterministic Kahn pass over the plan's DECLARED order
// (O2/I4). Not a map-iteration traversal and not a recursive DFS over map
// neighbours: an unstable sort makes a loop non-reproducible across runs
// from the same plan, and reproducibility from the pinned blueprint is the
// entire reason L6 pins it. "Deterministic algorithm over the plan's
// declared order" is the specification; TestSortIsStable runs it two hundred
// times and TestSortIsDeterministicAcrossRuns a hundred.

// stableSort returns the walk order, or the cycle's members when no order
// exists. The input slice is never mutated (TestLoadDoesNotMutateItsInput):
// the declaration order IS the stability reference, and a sort that
// rearranged it in place would corrupt the very order "stable" refers to.
//
// Algorithm: repeated left-to-right scans of the declaration for the first
// not-yet-placed task whose dependencies are all placed. Whenever several
// tasks are simultaneously eligible, the one declared FIRST goes next —
// which is exactly what "two tasks with no dependency between them keep
// their plan order" means under dependencies. O(n²) on purpose: plans are
// small, and the naive scan is what makes the stability argument checkable
// by reading it instead of by trusting a heap's tie-breaking.
func stableSort(tasks []Task) (sorted []Task, cycle []string) {
	n := len(tasks)
	if n == 0 {
		return []Task{}, nil
	}

	index := make(map[string]int, n)
	for i, t := range tasks {
		index[t.ID] = i
	}

	// Deduplicated edges: a dependency written twice is one edge. indeg is
	// the count of UNPLACED dependencies remaining.
	deps := make([][]int, n)
	indeg := make([]int, n)
	for i, t := range tasks {
		seen := make(map[int]bool, len(t.DependsOn))
		for _, d := range t.DependsOn {
			j, ok := index[d] // missing edges were refused before this ran
			if !ok || seen[j] || j == i {
				continue
			}
			seen[j] = true
			deps[i] = append(deps[i], j)
			indeg[i]++
		}
	}

	placed := make([]bool, n)
	sorted = make([]Task, 0, n)
	placedCount := 0
	for placedCount < n {
		picked := -1
		for i := 0; i < n; i++ {
			if !placed[i] && indeg[i] == 0 {
				picked = i
				break // FIRST in declaration order — the stability step
			}
		}
		if picked < 0 {
			break // nothing is ready: what remains is cyclic
		}
		placed[picked] = true
		sorted = append(sorted, tasks[picked])
		placedCount++
		for i := 0; i < n; i++ {
			if placed[i] {
				continue
			}
			for _, j := range deps[i] {
				if j == picked {
					indeg[i]--
				}
			}
		}
	}

	if placedCount < n {
		return nil, findCycle(tasks, index, deps, placed)
	}
	return sorted, nil
}

// findCycle walks out a real cycle from the stranded remainder and names its
// members in cycle order — "there is a cycle" is not an answer; it sends
// someone to read a twenty-task plan by hand. The start is the FIRST
// stranded task in declaration order, so the named cycle is deterministic;
// walking each task's first stranded dependency (declaration order again)
// must eventually repeat a node, and the repeated segment is the cycle.
//
// Downstream tasks are NOT members: a task that merely depends on the cycle
// is stranded by it, and naming it as a member would send the reader to the
// wrong edge (TestCycleIsNamed keeps T4 out of the sentence for exactly
// this reason).
func findCycle(tasks []Task, index map[string]int, deps [][]int, placed []bool) []string {
	// The stranded set is "not placed".
	stranded := func(i int) bool { return !placed[i] }

	start := -1
	for i := range tasks {
		if stranded(i) {
			start = i
			break
		}
	}

	// Walk first-stranded-dependency edges until a node repeats. Every
	// stranded node has at least one stranded dependency (all its
	// non-stranded ones were placed and decremented to zero — a zero-indeg
	// stranded node would have been picked), so the walk cannot fall off
	// the graph.
	order := []int{}
	position := make(map[int]int, len(tasks))
	cur := start
	for {
		if pos, ok := position[cur]; ok {
			// The segment from the first occurrence of cur to the end IS
			// the cycle.
			loop := order[pos:]
			names := make([]string, 0, len(loop))
			for _, i := range loop {
				names = append(names, tasks[i].ID)
			}
			return names
		}
		position[cur] = len(order)
		order = append(order, cur)
		for _, j := range deps[cur] {
			if stranded(j) {
				cur = j
				break
			}
		}
	}
}
