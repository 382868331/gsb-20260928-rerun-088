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
