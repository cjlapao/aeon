package learnings

import (
	"fmt"
	"math"
	"sort"
)

// The knowledge map (the app's Memory page): where each learning sits when the learnings are laid out by
// what they MEAN, and which learnings are nearest to which.
//
// Two honest limits are carried in the answer rather than hidden:
//
//   - A learning with no stored vector has no position. The embedding pass (learnings.embed) fills the
//     vectors; until it has run, a learning is simply absent from the map, and the answer counts how many
//     are (Pending) and whether a route exists to embed them (CanEmbed). Nothing is positioned by a guess.
//   - The map is a 2-D shadow of a high-dimensional space. It is a principal-component projection of the
//     centred vectors, so distance on the map is a hint and the Links (cosine similarity in the FULL
//     space) are the evidence. Basis says so in the daemon's own words.
//
// The projection is deterministic: the same vectors always give the same map, so a person who comes back
// finds their cluster where they left it.

const (
	// MaxMapPoints caps the learnings laid out at once. The Gram matrix is n×n and costs n²·d to build, so
	// the cap keeps the call bounded; the newest learnings are kept and the answer says how many were left out.
	MaxMapPoints = 600
	// DefaultMapNeighbours is how many nearest neighbours each learning links to.
	DefaultMapNeighbours = 3
	// MaxMapNeighbours bounds the ask.
	MaxMapNeighbours = 8
	// MinMapSimilarity is the cosine below which a nearest neighbour is not worth drawing a line to.
	MinMapSimilarity = 0.5
	// mapScale is the half-extent the projected points are scaled into (the kit's canvas fits any extent;
	// a fixed one keeps the numbers readable on the wire).
	mapScale = 300.0
)

// MapPoint is one positioned learning.
type MapPoint struct {
	ID int64
	X  float64
	Y  float64
}

// MapLink joins two learnings that are nearest neighbours in the full vector space.
type MapLink struct {
	A          int64
	B          int64
	Similarity float64
}

// MapResult is the map and its honest accounting.
type MapResult struct {
	Projected bool
	Model     string
	Dim       int
	Total     int // learnings the filters selected
	Embedded  int // of those, how many have a stored vector
	Omitted   int // embedded but left off because of MaxMapPoints
	CanEmbed  bool
	Points    []MapPoint
	Links     []MapLink
	Basis     string
}

// Map lays out the learnings the filter selects. It reads the same set the board reads (so the map and
// the board can never disagree about what is on screen), joins it to the stored vectors, and projects.
func (l *Learnings) Map(f BoardFilter, neighbours int) (*MapResult, error) {
	rows, err := l.Board(f)
	if err != nil {
		return nil, err
	}
	if neighbours <= 0 {
		neighbours = DefaultMapNeighbours
	}
	if neighbours > MaxMapNeighbours {
		neighbours = MaxMapNeighbours
	}
	res := &MapResult{Total: len(rows), CanEmbed: l.route != nil && l.embedder != nil}
	model := ""
	if l.route != nil {
		model = l.route.Model
	}
	set, err := l.s.ReadLearningVectors(model)
	if err != nil {
		return nil, err
	}
	if set == nil || len(set.Vectors) == 0 {
		res.Basis = noVectorsBasis(res.CanEmbed)
		return res, nil
	}
	res.Model = model
	res.Dim = set.Dim

	var ids []int64
	var vecs [][]float32
	for _, lr := range rows {
		if v, ok := set.Vectors[lr.ID]; ok {
			ids = append(ids, lr.ID)
			vecs = append(vecs, v)
		}
	}
	res.Embedded = len(ids)
	if len(ids) < 3 {
		res.Basis = fmt.Sprintf("only %d of %d learnings have a vector — a map needs at least 3 to have a shape; run the embedding pass to place the rest", len(ids), res.Total)
		return res, nil
	}
	if len(ids) > MaxMapPoints {
		// Keep the newest (highest ids): they are what a person is most likely looking for.
		order := make([]int, len(ids))
		for i := range order {
			order[i] = i
		}
		sort.Slice(order, func(a, b int) bool { return ids[order[a]] > ids[order[b]] })
		order = order[:MaxMapPoints]
		sort.Ints(order)
		keptIDs := make([]int64, 0, MaxMapPoints)
		keptVecs := make([][]float32, 0, MaxMapPoints)
		for _, i := range order {
			keptIDs = append(keptIDs, ids[i])
			keptVecs = append(keptVecs, vecs[i])
		}
		res.Omitted = len(ids) - len(keptIDs)
		ids, vecs = keptIDs, keptVecs
	}
	res.Points, res.Links = ProjectVectors(ids, vecs, neighbours)
	res.Projected = len(res.Points) > 0
	res.Basis = projectedBasis(res)
	return res, nil
}

func noVectorsBasis(canEmbed bool) string {
	if canEmbed {
		return "no learning has a vector yet — run the embedding pass (learnings.embed) so the map can place them by meaning"
	}
	return "no learning has a vector and no embedding route is configured — the map cannot place them by meaning (D5: no route is the normal state)"
}

func projectedBasis(r *MapResult) string {
	b := fmt.Sprintf("positions are a 2-D principal-component projection of the stored %d-dimensional vectors (deterministic); lines join each learning to its nearest neighbours by cosine similarity in the full space, above %.2f", r.Dim, MinMapSimilarity)
	if pending := r.Total - r.Embedded; pending > 0 {
		b += fmt.Sprintf("; %d of %d learnings have no vector yet and are not on the map", pending, r.Total)
	}
	if r.Omitted > 0 {
		b += fmt.Sprintf("; %d older learnings were left off to keep the map bounded (newest %d shown)", r.Omitted, MaxMapPoints)
	}
	return b
}

// ProjectVectors positions each vector in 2-D by principal-component analysis and finds each one's nearest
// neighbours. It works from the n×n Gram matrix of the centred vectors (the kernel trick) because n is
// bounded and the dimension is not: the top two eigenvectors of that matrix, scaled by the square roots of
// their eigenvalues, ARE the first two principal coordinates. Pure, deterministic, no random numbers.
func ProjectVectors(ids []int64, vecs [][]float32, neighbours int) ([]MapPoint, []MapLink) {
	n := len(ids)
	if n == 0 || n != len(vecs) {
		return nil, nil
	}
	d := len(vecs[0])
	// Unit-length rows: cosine similarity is then a dot product.
	u := make([][]float64, n)
	for i, v := range vecs {
		row := make([]float64, d)
		var norm float64
		for j, x := range v {
			row[j] = float64(x)
			norm += float64(x) * float64(x)
		}
		norm = math.Sqrt(norm)
		if norm > 0 {
			for j := range row {
				row[j] /= norm
			}
		}
		u[i] = row
	}
	// Gram matrix of the unit vectors: g[i][j] = cosine(i, j).
	g := make([][]float64, n)
	for i := range g {
		g[i] = make([]float64, n)
	}
	for i := 0; i < n; i++ {
		g[i][i] = dot(u[i], u[i])
		for j := i + 1; j < n; j++ {
			c := dot(u[i], u[j])
			g[i][j], g[j][i] = c, c
		}
	}
	links := nearestLinks(ids, g, neighbours)

	// Double-centre the Gram matrix: the Gram matrix of the mean-subtracted vectors.
	rowMean := make([]float64, n)
	var total float64
	for i := 0; i < n; i++ {
		var sum float64
		for j := 0; j < n; j++ {
			sum += g[i][j]
		}
		rowMean[i] = sum / float64(n)
		total += sum
	}
	total /= float64(n * n)
	c := make([][]float64, n)
	for i := 0; i < n; i++ {
		c[i] = make([]float64, n)
		for j := 0; j < n; j++ {
			c[i][j] = g[i][j] - rowMean[i] - rowMean[j] + total
		}
	}
	v1, l1 := topEigen(c, nil)
	v2, l2 := topEigen(c, v1)
	pts := make([]MapPoint, n)
	s1, s2 := math.Sqrt(math.Max(l1, 0)), math.Sqrt(math.Max(l2, 0))
	var maxAbs float64
	for i := 0; i < n; i++ {
		pts[i] = MapPoint{ID: ids[i], X: v1[i] * s1, Y: v2[i] * s2}
		maxAbs = math.Max(maxAbs, math.Max(math.Abs(pts[i].X), math.Abs(pts[i].Y)))
	}
	if maxAbs > 0 {
		for i := range pts {
			pts[i].X = round2(pts[i].X / maxAbs * mapScale)
			pts[i].Y = round2(pts[i].Y / maxAbs * mapScale)
		}
	}
	return pts, links
}

func dot(a, b []float64) float64 {
	var s float64
	for i := range a {
		s += a[i] * b[i]
	}
	return s
}

func round2(x float64) float64 { return math.Round(x*100) / 100 }

// nearestLinks keeps each learning's k most similar others above the floor, once per pair.
func nearestLinks(ids []int64, g [][]float64, k int) []MapLink {
	n := len(ids)
	seen := map[[2]int64]bool{}
	var out []MapLink
	for i := 0; i < n; i++ {
		type cand struct {
			j   int
			sim float64
		}
		cands := make([]cand, 0, n-1)
		for j := 0; j < n; j++ {
			if j != i {
				cands = append(cands, cand{j, g[i][j]})
			}
		}
		sort.Slice(cands, func(a, b int) bool {
			if cands[a].sim != cands[b].sim {
				return cands[a].sim > cands[b].sim
			}
			return ids[cands[a].j] < ids[cands[b].j]
		})
		for _, c := range cands {
			if c.sim < MinMapSimilarity {
				break
			}
			a, b := ids[i], ids[c.j]
			if a > b {
				a, b = b, a
			}
			key := [2]int64{a, b}
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, MapLink{A: a, B: b, Similarity: round2(c.sim)})
			if countFor(out, ids[i]) >= k {
				break
			}
		}
	}
	sort.Slice(out, func(a, b int) bool {
		if out[a].A != out[b].A {
			return out[a].A < out[b].A
		}
		return out[a].B < out[b].B
	})
	return out
}

func countFor(links []MapLink, id int64) int {
	n := 0
	for _, l := range links {
		if l.A == id || l.B == id {
			n++
		}
	}
	return n
}

// topEigen is power iteration for the dominant eigenpair of a symmetric matrix, with the first
// eigenvector projected out when given (deflation by orthogonalisation). The start vector is fixed, so
// the answer is the same every run; its sign is fixed too (the largest-magnitude entry is made positive).
func topEigen(m [][]float64, orthogonalTo []float64) ([]float64, float64) {
	n := len(m)
	v := make([]float64, n)
	for i := range v {
		v[i] = 1 + 0.01*float64((i*7919)%101)
	}
	normalise := func(x []float64) float64 {
		var s float64
		for _, e := range x {
			s += e * e
		}
		s = math.Sqrt(s)
		if s > 0 {
			for i := range x {
				x[i] /= s
			}
		}
		return s
	}
	orthogonalise := func(x []float64) {
		if orthogonalTo == nil {
			return
		}
		p := dot(x, orthogonalTo)
		for i := range x {
			x[i] -= p * orthogonalTo[i]
		}
	}
	orthogonalise(v)
	normalise(v)
	var lambda float64
	for iter := 0; iter < 200; iter++ {
		w := make([]float64, n)
		for i := 0; i < n; i++ {
			w[i] = dot(m[i], v)
		}
		orthogonalise(w)
		lambda = normalise(w)
		var diff float64
		for i := range v {
			diff += math.Abs(w[i] - v[i])
		}
		v = w
		if diff < 1e-10 {
			break
		}
	}
	// A fixed sign: the largest-magnitude component is positive.
	var big float64
	for _, e := range v {
		if math.Abs(e) > math.Abs(big) {
			big = e
		}
	}
	if big < 0 {
		for i := range v {
			v[i] = -v[i]
		}
	}
	return v, lambda
}
