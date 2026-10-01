package analysis

import (
	"testing"
	"time"

	"performance-investigation/internal/model"
)

func TestParallelChildrenUseUnionAndMarkDuplicates(t *testing.T) {
	start := time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC)
	batch := model.Batch{Records: []model.CallRecord{
		record("root", "trace", "", "a", start, start.Add(time.Second)),
		record("first", "trace", "root", "a", start.Add(100*time.Millisecond), start.Add(700*time.Millisecond)),
		record("second", "trace", "root", "a", start.Add(300*time.Millisecond), start.Add(600*time.Millisecond)),
		record("first", "trace", "root", "a", start.Add(100*time.Millisecond), start.Add(700*time.Millisecond)),
		record("orphan", "other", "missing", "c", start.Add(2*time.Second), start.Add(2300*time.Millisecond)),
	}}

	result := Analyze(batch)
	root := result.Traces["trace"]
	if root == nil {
		t.Fatal("trace root missing")
	}
	if got := root.Timing.WaitMS.Nominal; got != 600 {
		t.Fatalf("wait = %v, want 600", got)
	}
	if got := root.Timing.ExecutionMS.Nominal; got != 400 {
		t.Fatalf("execution = %v, want 400", got)
	}
	if !root.Timing.Parallel {
		t.Fatal("overlapping children should be marked parallel")
	}
	if !findRecord(result.Batch.Records, "first").Duplicate {
		t.Fatal("duplicate record should be flagged")
	}
	if !hasAnomaly(result.Anomalies, "missing_parent") || !hasAnomaly(result.Anomalies, "duplicate_record") {
		t.Fatalf("expected anomalies, got %#v", result.Anomalies)
	}
}

func TestCrossDeviceTimingStaysUncertain(t *testing.T) {
	start := time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC)
	batch := model.Batch{Records: []model.CallRecord{
		record("root", "trace", "", "a", start, start.Add(time.Second)),
		record("remote", "trace", "root", "b", start.Add(200*time.Millisecond), start.Add(800*time.Millisecond)),
	}}
	root := Analyze(batch).Traces["trace"]
	if root.Timing.WaitMS.Certain {
		t.Fatal("cross-device wait must not be reported as exact")
	}
	if root.Timing.WaitMS.Lower != 600 {
		t.Fatalf("wait lower bound = %v, want 600", root.Timing.WaitMS.Lower)
	}
	if root.Timing.ExecutionMS.Lower != 0 || root.Timing.ExecutionMS.Upper != 400 {
		t.Fatalf("execution bounds = %v..%v, want 0..400", root.Timing.ExecutionMS.Lower, root.Timing.ExecutionMS.Upper)
	}
}

func TestMissingParentStaysVisibleUnderVirtualRoot(t *testing.T) {
	start := time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC)
	batch := model.Batch{Records: []model.CallRecord{
		record("root", "trace", "", "a", start, start.Add(time.Second)),
		record("orphan", "trace", "missing", "a", start.Add(2*time.Second), start.Add(2300*time.Millisecond)),
	}}
	result := Analyze(batch)
	root := result.Traces["trace"]
	if root == nil {
		t.Fatal("trace root missing")
	}
	if len(root.Children) != 2 {
		t.Fatalf("virtual root should keep both dangling roots, got %d children", len(root.Children))
	}
	if !hasAnomaly(result.Anomalies, "multiple_roots") || !hasAnomaly(result.Anomalies, "missing_parent") {
		t.Fatalf("expected multiple_roots and missing_parent anomalies, got %#v", result.Anomalies)
	}
	found := false
	for _, child := range root.Children {
		if child.Record.ID == "orphan" {
			found = true
		}
		if child.Children == nil {
			t.Fatalf("children of %s must serialize as empty array, not null", child.Record.ID)
		}
	}
	if !found {
		t.Fatal("orphan record dropped from tree")
	}
}

func TestLeafChildrenAreEmptyNotNil(t *testing.T) {
	start := time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC)
	batch := model.Batch{Records: []model.CallRecord{
		record("root", "trace", "", "a", start, start.Add(time.Second)),
		record("leaf", "trace", "root", "a", start.Add(100*time.Millisecond), start.Add(200*time.Millisecond)),
	}}
	root := Analyze(batch).Traces["trace"]
	if root.Children == nil || root.Children[0].Children == nil {
		t.Fatal("children must be empty slices so the page can render leaf nodes")
	}
}

func TestAnalyzeIsDeterministicAcrossRuns(t *testing.T) {
	start := time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC)
	records := []model.CallRecord{
		record("root", "trace", "", "a", start, start.Add(time.Second)),
		record("first", "trace", "root", "a", start.Add(100*time.Millisecond), start.Add(700*time.Millisecond)),
		record("second", "trace", "root", "a", start.Add(300*time.Millisecond), start.Add(600*time.Millisecond)),
		record("first", "trace", "root", "a", start.Add(100*time.Millisecond), start.Add(700*time.Millisecond)),
		record("orphan", "trace", "missing", "b", start.Add(2*time.Second), start.Add(2300*time.Millisecond)),
		record("remote", "trace", "root", "c", start.Add(200*time.Millisecond), start.Add(800*time.Millisecond)),
	}
	first := Analyze(model.Batch{Records: records})
	for run := 0; run < 20; run++ {
		next := Analyze(model.Batch{Records: records})
		if !equalSummaries(first.Requests, next.Requests) {
			t.Fatalf("run %d: summaries differ\nfirst: %#v\nnext:  %#v", run, first.Requests, next.Requests)
		}
		if !equalNodes(first.Traces["trace"], next.Traces["trace"]) {
			t.Fatalf("run %d: trace tree differs between runs", run)
		}
	}
}

func equalSummaries(a, b []model.RequestSummary) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func equalNodes(a, b *model.CallNode) bool {
	if a == nil || b == nil {
		return a == b
	}
	if a.Record.ID != b.Record.ID || a.Timing != b.Timing || len(a.Children) != len(b.Children) {
		return false
	}
	for i := range a.Children {
		if !equalNodes(a.Children[i], b.Children[i]) {
			return false
		}
	}
	return true
}

func record(id, trace, parent, device string, start, end time.Time) model.CallRecord {
	return model.CallRecord{ID: id, TraceID: trace, ParentID: parent, Service: device, Operation: "op", DeviceID: device, Start: start, End: end}
}

func findRecord(records []model.CallRecord, id string) model.CallRecord {
	for _, item := range records {
		if item.ID == id {
			return item
		}
	}
	return model.CallRecord{}
}

func hasAnomaly(items []model.Anomaly, kind string) bool {
	for _, item := range items {
		if item.Type == kind {
			return true
		}
	}
	return false
}
