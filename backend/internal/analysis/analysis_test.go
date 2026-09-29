package analysis

import (
	"testing"
	"time"

	"perfinvest/internal/model"
)

func ns(sec float64) int64 { return int64(sec * float64(time.Second)) }

func mk(id, pid, name string, start, end float64) model.Span {
	return model.Span{ID: id, ParentID: pid, TraceID: "t1", Name: name,
		StartNS: ns(start), EndNS: ns(end), Device: "dev-" + id}
}

func TestParallelChildrenNotSummed(t *testing.T) {
	spans := []model.Span{
		mk("root", "", "GET /checkout", 0, 10),
		mk("a", "root", "db.query", 2, 8),
		mk("b", "root", "http.pay", 3, 9),
	}
	tr := BuildTrace(spans, "t1", "")
	b := tr.Nodes[0].Breakdown
	if b.DurationNS != ns(10) {
		t.Fatalf("duration = %v, want 10s", b.DurationNS)
	}
	if b.WaitingNS != ns(7) {
		t.Fatalf("waiting = %v, want 7s (union, not sum)", b.WaitingNS)
	}
	if b.ExecutingNS != ns(3) {
		t.Fatalf("executing = %v, want 3s", b.ExecutingNS)
	}
	if b.OverlapNS != ns(12) {
		t.Fatalf("overlap = %v, want 12s", b.OverlapNS)
	}
}

func TestSequentialChildren(t *testing.T) {
	spans := []model.Span{
		mk("root", "", "root", 0, 10),
		mk("a", "root", "a", 1, 4),
		mk("b", "root", "b", 4, 8),
	}
	tr := BuildTrace(spans, "t1", "")
	b := tr.Nodes[0].Breakdown
	if b.WaitingNS != ns(7) || b.ExecutingNS != ns(3) {
		t.Fatalf("got wait=%v exec=%v, want 7s/3s", b.WaitingNS, b.ExecutingNS)
	}
	if b.OverlapNS != b.WaitingNS {
		t.Fatalf("sequential children should not show parallel overlap")
	}
}

func TestMissingParentFlagged(t *testing.T) {
	spans := []model.Span{
		mk("a", "ghost", "orphan", 0, 5),
		mk("b", "", "other", 0, 10),
	}
	tr := BuildTrace(spans, "t1", "")
	var flagged *model.Node
	for _, n := range tr.Nodes {
		if n.Span.ID == "a" {
			flagged = n
		}
	}
	if flagged == nil || !hasFlag(flagged, model.FlagMissingParent) {
		t.Fatalf("span with unresolved parent must be flagged")
	}
	if len(tr.Notes) == 0 {
		t.Fatalf("missing parent should produce an evidence note")
	}
}

func TestDuplicateKeptAndFlagged(t *testing.T) {
	d1 := mk("root", "", "root", 0, 10)
	d2 := mk("root", "", "root", 0, 11)
	spans := []model.Span{d1, d2, mk("a", "root", "a", 1, 2)}
	tr := BuildTrace(spans, "t1", "")
	root := tr.Nodes[0]
	if !hasFlag(root, model.FlagDuplicate) {
		t.Fatalf("duplicate record must be flagged")
	}
	if len(root.Dups) != 1 {
		t.Fatalf("duplicate raw record must be retained as evidence")
	}
	if root.Span.DurationNS() != ns(10) {
		t.Fatalf("first record must win for the tree")
	}
}

func TestClockBiasKeptNotForced(t *testing.T) {
	spans := []model.Span{
		mk("root", "", "root", 0, 10),
		mk("a", "root", "biased-child", 25, 27),
	}
	tr := BuildTrace(spans, "t1", "")
	b := tr.Nodes[0].Breakdown
	if b.WaitingNS != 0 {
		t.Fatalf("biased child outside window must not count as waiting, got %v", b.WaitingNS)
	}
	if b.ExecutingNS != ns(10) {
		t.Fatalf("parent should appear fully self-executing, got %v", b.ExecutingNS)
	}
	if b.OutsideNS < ns(1) {
		t.Fatalf("outside time beyond tolerance must be reported, got %v", b.OutsideNS)
	}
	if !b.ClockUncertain {
		t.Fatalf("clock bias must mark the breakdown uncertain")
	}
}

func TestNestedPartialOverlap(t *testing.T) {
	spans := []model.Span{
		mk("root", "", "root", 0, 10),
		mk("a", "root", "a", 2, 9),
		mk("g", "a", "g", 4, 6),
	}
	tr := BuildTrace(spans, "t1", "")
	root := tr.Nodes[0]
	a := root.Children[0]
	if a.Breakdown.WaitingNS != ns(2) || a.Breakdown.ExecutingNS != ns(5) {
		t.Fatalf("a: got wait=%v exec=%v want 2s/5s", a.Breakdown.WaitingNS, a.Breakdown.ExecutingNS)
	}
	if root.Breakdown.WaitingNS != ns(7) {
		t.Fatalf("root waiting counts direct children union, got %v", root.Breakdown.WaitingNS)
	}
}

func TestRecomputeDeterministic(t *testing.T) {
	spans := []model.Span{
		mk("root", "", "root", 0, 10), mk("a", "root", "a", 1, 5), mk("b", "root", "b", 4, 9)}
	first := BuildTrace(spans, "t1", "")
	second := BuildTrace(spans, "t1", "")
	if first.Nodes[0].Breakdown != second.Nodes[0].Breakdown {
		t.Fatalf("breakdown must be a pure function of raw spans")
	}
}
