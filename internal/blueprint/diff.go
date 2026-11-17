package blueprint

// diff.go — S48 T2 (BP7): a real unified diff with no dependency. A small
// LCS over the trimmed middle (common prefix/suffix cut first, so the
// quadratic table only sees what actually changed), emitted as hunks with
// three lines of context. Output is deterministic (the DP walks in one fixed
// direction) and empty for equal bytes. `blueprint.diff` (T7) and the
// history view consume this; a hunk application back onto `a` yields `b` —
// pinned by a round-trip test that PARSES this output.

import (
	"fmt"
	"strings"
)

// noNL marks a final line that had no trailing newline (part of the line's
// identity, so a file whose only change is its final newline diffs as a
// real one-line change and round-trips).
const noNL = "\x00\x00no-final-newline"

// Diff renders a unified diff of two byte blobs; equal inputs give "".
func Diff(a, b []byte, aLabel, bLabel string) string {
	if string(a) == string(b) {
		return ""
	}
	aLines := splitDiffLines(a)
	bLines := splitDiffLines(b)

	// Common prefix/suffix first: the LCS table then sees only the middle.
	lo := 0
	for lo < len(aLines) && lo < len(bLines) && aLines[lo] == bLines[lo] {
		lo++
	}
	hiA, hiB := len(aLines), len(bLines)
	for hiA > lo && hiB > lo && aLines[hiA-1] == bLines[hiB-1] {
		hiA--
		hiB--
	}

	ops := lcsOps(aLines[lo:hiA], bLines[lo:hiB])

	// The full edit sequence with side line numbers.
	var edits []edit
	ac, bc := 0, 0
	for _, l := range aLines[:lo] {
		ac++
		bc++
		edits = append(edits, edit{' ', l, ac, bc, ac == len(aLines), bc == len(bLines)})
	}
	for _, o := range ops {
		switch o.op {
		case ' ':
			ac++
			bc++
			edits = append(edits, edit{' ', o.s, ac, bc, ac == len(aLines), bc == len(bLines)})
		case '-':
			ac++
			edits = append(edits, edit{'-', o.s, ac, 0, ac == len(aLines), false})
		case '+':
			bc++
			edits = append(edits, edit{'+', o.s, 0, bc, false, bc == len(bLines)})
		}
	}
	for _, l := range aLines[hiA:] {
		ac++
		bc++
		edits = append(edits, edit{' ', l, ac, bc, ac == len(aLines), bc == len(bLines)})
	}

	// Mark the lines a hunk must carry: every change plus ctx of context
	// around each.
	const ctx = 3
	included := make([]bool, len(edits))
	for i, e := range edits {
		if e.op == ' ' {
			continue
		}
		for j := i - ctx; j <= i+ctx; j++ {
			if j >= 0 && j < len(included) {
				included[j] = true
			}
		}
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "--- %s\n+++ %s\n", aLabel, bLabel)
	for i := 0; i < len(edits); {
		if !included[i] {
			i++
			continue
		}
		j := i
		for j < len(edits) && included[j] {
			j++
		}
		hunk := edits[i:j]
		aCount, bCount := 0, 0
		for _, e := range hunk {
			if e.op != '+' {
				aCount++
			}
			if e.op != '-' {
				bCount++
			}
		}
		aStart, bStart := 0, 0
		for _, e := range hunk {
			if aStart == 0 && e.aNum > 0 {
				aStart = e.aNum
			}
			if bStart == 0 && e.bNum > 0 {
				bStart = e.bNum
			}
		}
		if aCount == 0 { // a pure insertion: the header names the line before
			aStart = prevSide(edits, i, true)
		}
		if bCount == 0 {
			bStart = prevSide(edits, i, false)
		}
		fmt.Fprintf(&sb, "@@ -%d,%d +%d,%d @@\n", aStart, aCount, bStart, bCount)
		for _, e := range hunk {
			sb.WriteByte(e.op)
			sb.WriteString(strings.TrimSuffix(e.s, noNL))
			sb.WriteByte('\n')
			if strings.HasSuffix(e.s, noNL) {
				sb.WriteString("\\ No newline at end of file\n")
			}
		}
		i = j
	}
	return sb.String()
}

// prevSide finds the last known side line number before index i (the line an
// empty-count hunk names: the one the insertion follows).
func prevSide(edits []edit, i int, sideA bool) int {
	for k := i - 1; k >= 0; k-- {
		if sideA && edits[k].aNum > 0 {
			return edits[k].aNum
		}
		if !sideA && edits[k].bNum > 0 {
			return edits[k].bNum
		}
	}
	return 0
}

// edit is one line of the edit sequence: which side(s) it lives on and the
// 1-based line numbers on each (0 = not on that side).
type edit struct {
	op           byte // ' ', '-', '+'
	s            string
	aNum, bNum   int
	lastA, lastB bool
}

type diffOp struct {
	op byte
	s  string
}

// lcsOps is the classic longest-common-subsequence table over the middle,
// walked back in one fixed direction (delete-before-insert on ties), so the
// output is deterministic. Above the size guard the middle is replaced
// wholesale — still a correct unified diff, never a quadratic stall.
func lcsOps(a, b []string) []diffOp {
	m, n := len(a), len(b)
	if m*n > 4_000_000 {
		var ops []diffOp
		for _, l := range a {
			ops = append(ops, diffOp{'-', l})
		}
		for _, l := range b {
			ops = append(ops, diffOp{'+', l})
		}
		return ops
	}
	dp := make([][]int, m+1)
	for i := range dp {
		dp[i] = make([]int, n+1)
	}
	for i := m - 1; i >= 0; i-- {
		for j := n - 1; j >= 0; j-- {
			if a[i] == b[j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else if dp[i+1][j] >= dp[i][j+1] {
				dp[i][j] = dp[i+1][j]
			} else {
				dp[i][j] = dp[i][j+1]
			}
		}
	}
	var ops []diffOp
	i, j := 0, 0
	for i < m && j < n {
		switch {
		case a[i] == b[j]:
			ops = append(ops, diffOp{' ', a[i]})
			i++
			j++
		case dp[i+1][j] >= dp[i][j+1]:
			ops = append(ops, diffOp{'-', a[i]})
			i++
		default:
			ops = append(ops, diffOp{'+', b[j]})
			j++
		}
	}
	for ; i < m; i++ {
		ops = append(ops, diffOp{'-', a[i]})
	}
	for ; j < n; j++ {
		ops = append(ops, diffOp{'+', b[j]})
	}
	return ops
}

// splitDiffLines splits on "\n" keeping "\r" (a CRLF file's lines carry it,
// so CRLF-vs-LF is a visible difference) and marks the final line when the
// blob had no trailing newline.
func splitDiffLines(s []byte) []string {
	str := string(s)
	if str == "" {
		return nil
	}
	parts := strings.Split(str, "\n")
	if parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	} else {
		parts[len(parts)-1] += noNL
	}
	return parts
}
