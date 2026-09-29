// Package analysis reconstructs the causal tree of one request from raw call
// records and explains where its elapsed time went.
//
// Design rules:
//   - Parallel children are merged by interval union: their time is never
//     simply added together.
//   - Cross-device clock bias is never papered over by rewriting timestamps.
//     Only the part of a child that provably lies inside the parent window
//     counts as waiting; the part outside is reported separately and flagged.
//   - Breakdowns are pure functions of the raw spans, so a saved investigation
//     always recomputes the same numbers when reopened.
package analysis

import (
	"sort"
	"time"

	"perfinvest/internal/model"
)

// DefaultSkewTolerance is the amount a child may protrude outside its parent's
// window (network/queueing jitter) without being flagged as a clock anomaly.
const DefaultSkewTolerance = int64(time.Millisecond)

// interval is a half-open [start, end) window in nanoseconds.
type interval struct{ start, end int64 }

func (i interval) len() int64 {
	if i.end < i.start {
		return 0
	}
	return i.end - i.start
}

// unionLen returns the total length covered by at least one interval
// (overlapping/parallel intervals counted once).
func unionLen(ivs []interval) int64 {
	if len(ivs) == 0 {
		return 0
	}
	sorted := make([]interval, len(ivs))
	copy(sorted, ivs)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].start != sorted[j].start {
			return sorted[i].start < sorted[j].start
		}
		return sorted[i].end < sorted[j].end
	})
	total := int64(0)
	cur := sorted[0]
	for _, iv := range sorted[1:] {
		if iv.start <= cur.end {
			if iv.end > cur.end {
				cur.end = iv.end
			}
			continue
		}
		total += cur.len()
		cur = iv
	}
	total += cur.len()
	return total
}

// intersect clips iv to the window [start, end).
func intersect(iv interval, start, end int64) interval {
	if iv.start < start {
		iv.start = start
	}
	if iv.end > end {
		iv.end = end
	}
	if iv.end < iv.start {
		iv.end = iv.start
	}
	return iv
}

// BuildTrace reconstructs one request trace from its spans.
// rootSpanID may be empty to pick the natural (parent-less) root.
func BuildTrace(spans []model.Span, traceID, rootSpanID string) *model.Trace {
	t := &model.Trace{TraceID: traceID}

	// 1. Deduplicate by id: first record wins, later ones are evidence kept
	//    aside and flagged as duplicates rather than silently dropped.
	byID := map[string]*model.Node{}
	var nodes []*model.Node
	seen := map[string]bool{}
	for _, sp := range spans {
		if seen[sp.ID] {
			if n := byID[sp.ID]; n != nil {
				n.Flags = addFlag(n.Flags, model.FlagDuplicate)
				n.Dups = append(n.Dups, sp)
			}
			continue
		}
		seen[sp.ID] = true
		n := &model.Node{Span: sp}
		if sp.DurationNS() <= 0 {
			n.Flags = addFlag(n.Flags, model.FlagClockAnomaly)
		}
		byID[sp.ID] = n
		nodes = append(nodes, n)
	}

	// 2. Link children to parents. A parent id that cannot be resolved is
	//    preserved: the child is flagged as missing_parent and treated as an
	//    inferred root instead of inventing a parent.
	var roots []*model.Node
	for _, n := range nodes {
		pid := n.Span.ParentID
		if pid == "" || byID[pid] == nil || byID[pid] == n {
			if pid != "" && byID[pid] == nil {
				n.Flags = addFlag(n.Flags, model.FlagMissingParent)
				t.Notes = append(t.Notes,
					"span "+n.Span.ID+" ("+n.Span.Name+") references missing parent "+pid)
			}
			roots = append(roots, n)
			continue
		}
		byID[pid].Children = append(byID[pid].Children, n)
	}

	// 3. Guard against id cycles that would make traversal non-terminating.
	roots = breakCycles(roots, t)

	// 4. Deterministic order: by start time, then id.
	sortNodes(roots)
	for _, n := range nodes {
		sortNodes(n.Children)
	}

	// 5. Choose the presented root.
	root := selectRoot(roots, rootSpanID)
	if root == nil {
		return t
	}
	t.RootSpanID = root.Span.ID

	// 6. Walk every connected component so orphans (missing parents) remain
	//    visible evidence. The chosen root comes first; the other components
	//    are shown after it instead of being force-attached.
	ordered := append([]*model.Node{root}, withoutNode(roots, root)...)
	t.Roots = ordered
	var walk func(n *model.Node, depth int)
	walk = func(n *model.Node, depth int) {
		n.Depth = depth
		t.Nodes = append(t.Nodes, n)
		n.Breakdown = breakdown(n, DefaultSkewTolerance)
		for _, c := range n.Children {
			walk(c, depth+1)
		}
	}
	for i, r := range ordered {
		walk(r, 0)
		if i > 0 {
			t.Notes = append(t.Notes,
				"span "+r.Span.ID+" ("+r.Span.Name+") is not connected to root "+root.Span.ID)
		}
	}
	t.RootSummary = &root.Breakdown
	return t
}

func withoutNode(ns []*model.Node, skip *model.Node) []*model.Node {
	var out []*model.Node
	for _, n := range ns {
		if n != skip {
			out = append(out, n)
		}
	}
	return out
}

func addFlag(flags []model.NodeFlag, f model.NodeFlag) []model.NodeFlag {
	for _, x := range flags {
		if x == f {
			return flags
		}
	}
	return append(flags, f)
}

func sortNodes(ns []*model.Node) {
	sort.Slice(ns, func(i, j int) bool {
		if ns[i].Span.StartNS != ns[j].Span.StartNS {
			return ns[i].Span.StartNS < ns[j].Span.StartNS
		}
		return ns[i].Span.ID < ns[j].Span.ID
	})
}

func selectRoot(roots []*model.Node, preferred string) *model.Node {
	if preferred != "" {
		for _, r := range roots {
			if r.Span.ID == preferred {
				return r
			}
		}
	}
	// Prefer a genuine root (no missing-parent flag) with the widest window.
	var best *model.Node
	for _, r := range roots {
		missing := false
		for _, f := range r.Flags {
			if f == model.FlagMissingParent {
				missing = true
			}
		}
		if best == nil || (!missing && hasFlag(best, model.FlagMissingParent)) ||
			((missing == hasFlag(best, model.FlagMissingParent)) && r.Span.DurationNS() > best.Span.DurationNS()) {
			best = r
		}
	}
	return best
}

func hasFlag(n *model.Node, f model.NodeFlag) bool {
	for _, x := range n.Flags {
		if x == f {
			return true
		}
	}
	return false
}

// breakCycles detaches edges that participate in id cycles and flags them.
func breakCycles(roots []*model.Node, t *model.Trace) []*model.Node {
	var detached []*model.Node
	var dfs func(n *model.Node, chain map[*model.Node]bool)
	dfs = func(n *model.Node, chain map[*model.Node]bool) {
		chain[n] = true
		kept := n.Children[:0]
		for _, c := range n.Children {
			if chain[c] {
				t.Notes = append(t.Notes,
					"cycle detected: detached edge "+n.Span.ID+" -> "+c.Span.ID)
				c.Flags = addFlag(c.Flags, model.FlagMissingParent)
				detached = append(detached, c)
				continue
			}
			kept = append(kept, c)
			dfs(c, chain)
		}
		n.Children = kept
		delete(chain, n)
	}
	for _, r := range roots {
		dfs(r, map[*model.Node]bool{})
	}
	return append(roots, detached...)
}

// breakdown explains one span's elapsed time.
//
//	waiting   = union of child windows clipped into the parent window
//	executing = parent duration - waiting
//	overlap   = raw sum of clipped child windows (>waiting means parallelism)
//	outside   = child time provably outside the parent window (clock bias)
func breakdown(n *model.Node, tolerance int64) model.Breakdown {
	dur := n.Span.DurationNS()
	b := model.Breakdown{DurationNS: dur, Recomputed: true}
	if dur <= 0 {
		b.UnknownNS = 0
		b.ClockUncertain = true
		return b
	}
	window := interval{n.Span.StartNS, n.Span.EndNS}
	var clipped []interval
	outside := int64(0)
	anomaly := false
	for _, c := range n.Children {
		childIV := interval{c.Span.StartNS, c.Span.EndNS}
		childLen := childIV.len()
		clip := intersect(childIV, window.start, window.end)
		if clip.len() > 0 {
			clipped = append(clipped, clip)
		}
		out := childLen - clip.len()
		if out > tolerance {
			anomaly = true
		}
		if out > tolerance {
			outside += out - tolerance
		}
	}
	b.WaitingNS = unionLen(clipped)
	b.OverlapNS = sumLen(clipped)
	b.OutsideNS = outside
	b.ExecutingNS = dur - b.WaitingNS
	if b.ExecutingNS < 0 {
		b.ExecutingNS = 0
	}
	b.ClockUncertain = anomaly
	if anomaly {
		n.Flags = addFlag(n.Flags, model.FlagClockAnomaly)
	}
	return b
}

// GroupByTrace partitions imported spans by trace id. Spans without a trace
// id are grouped by their connected parent graph; here they fall into a
// synthetic group so they remain importable.
func GroupByTrace(spans []model.Span) map[string][]model.Span {
	g := map[string][]model.Span{}
	for _, s := range spans {
		id := s.TraceID
		if id == "" {
			id = "(no-trace)"
		}
		g[id] = append(g[id], s)
	}
	return g
}

func sumLen(ivs []interval) int64 {
	var n int64
	for _, iv := range ivs {
		n += iv.len()
	}
	return n
}
