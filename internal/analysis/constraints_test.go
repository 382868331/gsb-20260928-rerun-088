package analysis

import (
	"testing"
	"time"

	"performance-investigation/internal/model"
)

func ptr(v float64) *float64 { return &v }

func offsetConstraint(id, a, b string, low, high float64) model.ClockConstraint {
	return model.ClockConstraint{ID: id, Type: "offset_range", ARecordID: a, BRecordID: b, LowerMS: ptr(low), UpperMS: ptr(high)}
}

func crossDeviceBatch(start time.Time) model.Batch {
	return model.Batch{Records: []model.CallRecord{
		record("root", "trace", "", "a", start, start.Add(time.Second)),
		record("remote", "trace", "root", "b", start.Add(200*time.Millisecond), start.Add(800*time.Millisecond)),
	}}
}

func TestOffsetRangeConstraintTightensBounds(t *testing.T) {
	start := time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC)
	constraint := offsetConstraint("c1", "root", "remote", 150, 250)
	root := AnalyzeWithConstraints(crossDeviceBatch(start), []model.ClockConstraint{constraint}).Traces["trace"]
	wait := root.Timing.WaitMS
	if wait.Certain {
		t.Fatal("range constraint must keep wait uncertain")
	}
	if wait.Lower != 500 || wait.Upper != 600 {
		t.Fatalf("wait bounds = %v..%v, want 500..600", wait.Lower, wait.Upper)
	}
	var remote *model.CallNode
	walk(root, func(n *model.CallNode) {
		if n.Record.ID == "remote" {
			remote = n
		}
	})
	if remote.Timing.StartMS.Lower != 150 || remote.Timing.StartMS.Upper != 250 {
		t.Fatalf("remote start bounds = %+v, want 150..250", remote.Timing.StartMS)
	}
	if remote.Timing.EndMS.Lower != 750 || remote.Timing.EndMS.Upper != 850 {
		t.Fatalf("remote end bounds = %+v, want 750..850", remote.Timing.EndMS)
	}
}

func TestExactOffsetConstraintMakesWaitCertain(t *testing.T) {
	start := time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC)
	constraint := offsetConstraint("c1", "root", "remote", 200, 200)
	root := AnalyzeWithConstraints(crossDeviceBatch(start), []model.ClockConstraint{constraint}).Traces["trace"]
	if !root.Timing.WaitMS.Certain || root.Timing.WaitMS.Nominal != 600 {
		t.Fatalf("exact offset should give certain wait 600, got %+v", root.Timing.WaitMS)
	}
}

func TestConflictingConstraintsNameInvolvedRecords(t *testing.T) {
	start := time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC)
	constraints := []model.ClockConstraint{
		offsetConstraint("c1", "root", "remote", 150, 160),
		offsetConstraint("c2", "root", "remote", 250, 260),
	}
	result := AnalyzeWithConstraints(crossDeviceBatch(start), constraints)
	var conflict *model.Anomaly
	for index := range result.Anomalies {
		if result.Anomalies[index].Type == "constraint_conflict" {
			conflict = &result.Anomalies[index]
		}
	}
	if conflict == nil {
		t.Fatalf("expected constraint_conflict anomaly, got %#v", result.Anomalies)
	}
	if len(conflict.RecordIDs) != 2 || conflict.RecordIDs[0] != "remote" || conflict.RecordIDs[1] != "root" {
		t.Fatalf("conflict must name involved records, got %v", conflict.RecordIDs)
	}
	if len(conflict.ConstraintIDs) != 2 {
		t.Fatalf("conflict must name involved constraints, got %v", conflict.ConstraintIDs)
	}
	flagged := findRecord(result.Batch.Records, "remote")
	found := false
	for _, item := range flagged.Anomalies {
		if item == "constraint_conflict" {
			found = true
		}
	}
	if !found {
		t.Fatalf("involved record should carry constraint_conflict flag, got %v", flagged.Anomalies)
	}
}

func TestParallelAndMissingParentWithConstraintsNeverSum(t *testing.T) {
	start := time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC)
	batch := model.Batch{Records: []model.CallRecord{
		record("root", "trace", "", "a", start, start.Add(time.Second)),
		record("first", "trace", "root", "b", start.Add(100*time.Millisecond), start.Add(600*time.Millisecond)),
		record("second", "trace", "root", "b", start.Add(300*time.Millisecond), start.Add(800*time.Millisecond)),
		record("orphan", "other", "missing", "c", start.Add(2*time.Second), start.Add(2500*time.Millisecond)),
	}}
	constraints := []model.ClockConstraint{offsetConstraint("c1", "root", "first", 100, 100)}
	result := AnalyzeWithConstraints(batch, constraints)
	root := result.Traces["trace"]
	if !root.Timing.Parallel {
		t.Fatal("overlapping children should still be marked parallel")
	}
	if root.Timing.WaitMS.Upper > 1000 {
		t.Fatalf("wait upper must never exceed parent duration by summing segments, got %v", root.Timing.WaitMS.Upper)
	}
	if root.Timing.WaitMS.Nominal != 700 {
		t.Fatalf("parallel children with exact offset should merge to union 700, got %v", root.Timing.WaitMS.Nominal)
	}
	if !hasAnomaly(result.Anomalies, "missing_parent") {
		t.Fatal("missing parent anomaly must be preserved alongside constraints")
	}
}

func TestCompareKeepsUndecidableWaitShareRanking(t *testing.T) {
	start := time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC)
	batch := model.Batch{Records: []model.CallRecord{
		record("root-1", "trace-1", "", "a", start, start.Add(time.Second)),
		record("remote-1", "trace-1", "root-1", "b", start.Add(200*time.Millisecond), start.Add(800*time.Millisecond)),
		record("root-2", "trace-2", "", "a", start, start.Add(time.Second)),
		record("remote-2", "trace-2", "root-2", "c", start.Add(100*time.Millisecond), start.Add(700*time.Millisecond)),
	}}
	invA := model.Investigation{ID: "inv-a", Name: "基线", RootID: "root-1", TraceID: "trace-1"}
	invB := model.Investigation{ID: "inv-b", Name: "对照", RootID: "root-1", TraceID: "trace-1"}
	comparison, err := Compare(Analyze(batch), Analyze(batch), invA, invB)
	if err != nil {
		t.Fatal(err)
	}
	if len(comparison.Requests) != 2 {
		t.Fatalf("expected per-request comparison, got %#v", comparison.Requests)
	}
	undecidable := 0
	for _, item := range comparison.Requests {
		if item.OrderUndecidable {
			undecidable++
		}
	}
	if undecidable != 2 {
		t.Fatalf("overlapping wait-share intervals must stay undecidable, got %#v", comparison.Requests)
	}
	if len(comparison.RankingNotes) == 0 {
		t.Fatal("expected ranking notes explaining undecidable ordering")
	}
	if comparison.AssumptionChange == "" {
		t.Fatal("expected assumption change description")
	}
}
