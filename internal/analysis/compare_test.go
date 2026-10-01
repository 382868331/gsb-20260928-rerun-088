package analysis

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"performance-investigation/internal/model"
)

func fixtureBatch(start time.Time) model.Batch {
	return model.Batch{Records: []model.CallRecord{
		record("root", "trace", "", "a", start, start.Add(time.Second)),
		record("first", "trace", "root", "a", start.Add(100*time.Millisecond), start.Add(700*time.Millisecond)),
		record("second", "trace", "root", "a", start.Add(300*time.Millisecond), start.Add(600*time.Millisecond)),
		record("first", "trace", "root", "a", start.Add(100*time.Millisecond), start.Add(700*time.Millisecond)),
		record("remote", "trace", "root", "b", start.Add(200*time.Millisecond), start.Add(800*time.Millisecond)),
		record("orphan", "other", "missing", "c", start.Add(2*time.Second), start.Add(2300*time.Millisecond)),
	}}
}

func TestLeafChildrenSerializeAsEmptyArray(t *testing.T) {
	start := time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC)
	result := Analyze(fixtureBatch(start))
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), `"children":null`) {
		t.Fatal("leaf children must serialize as [], not null")
	}
	if !strings.Contains(string(data), `"children":[]`) {
		t.Fatal("expected empty children arrays in analysis JSON")
	}
}

func TestCompareAttributesExecutionGrowth(t *testing.T) {
	start := time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC)
	slower := fixtureBatch(start)
	for index := range slower.Records {
		if slower.Records[index].ID == "root" {
			slower.Records[index].End = start.Add(1500 * time.Millisecond)
		}
	}
	invA := model.Investigation{ID: "inv-a", Name: "基线", RootID: "root", TraceID: "trace"}
	invB := model.Investigation{ID: "inv-b", Name: "回归", RootID: "root", TraceID: "trace"}
	comparison, err := Compare(Analyze(fixtureBatch(start)), Analyze(slower), invA, invB)
	if err != nil {
		t.Fatal(err)
	}
	if comparison.DurationDeltaMS != 500 {
		t.Fatalf("duration delta = %v, want 500", comparison.DurationDeltaMS)
	}
	if comparison.Attribution != "uncertain" {
		t.Fatalf("cross-device fixture must stay uncertain, got %q", comparison.Attribution)
	}
	if !comparison.EvidenceIncomplete {
		t.Fatal("duplicate/missing-parent fixture must flag incomplete evidence")
	}
	if len(comparison.Notes) == 0 {
		t.Fatal("expected uncertainty notes to be preserved")
	}
	if comparison.WaitDelta.Lower > comparison.WaitDelta.Nominal || comparison.WaitDelta.Upper < comparison.WaitDelta.Nominal {
		t.Fatalf("wait delta bounds inconsistent: %+v", comparison.WaitDelta)
	}
}

func TestCompareSameDeviceAttributesExecution(t *testing.T) {
	start := time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC)
	base := model.Batch{Records: []model.CallRecord{
		record("root", "trace", "", "a", start, start.Add(time.Second)),
		record("child", "trace", "root", "a", start.Add(100*time.Millisecond), start.Add(400*time.Millisecond)),
	}}
	slower := model.Batch{Records: []model.CallRecord{
		record("root", "trace", "", "a", start, start.Add(1400*time.Millisecond)),
		record("child", "trace", "root", "a", start.Add(100*time.Millisecond), start.Add(400*time.Millisecond)),
	}}
	invA := model.Investigation{ID: "inv-a", Name: "基线", RootID: "root", TraceID: "trace"}
	invB := model.Investigation{ID: "inv-b", Name: "回归", RootID: "root", TraceID: "trace"}
	comparison, err := Compare(Analyze(base), Analyze(slower), invA, invB)
	if err != nil {
		t.Fatal(err)
	}
	if comparison.Attribution != "execution" {
		t.Fatalf("growth should attribute to execution, got %q", comparison.Attribution)
	}
	if comparison.ExecutionDelta.Nominal != 400 || !comparison.ExecutionDelta.Certain {
		t.Fatalf("execution delta = %+v, want certain 400", comparison.ExecutionDelta)
	}
	if comparison.WaitDelta.Nominal != 0 {
		t.Fatalf("wait delta = %v, want 0", comparison.WaitDelta.Nominal)
	}
}

func TestCompareMissingRootFails(t *testing.T) {
	start := time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC)
	result := Analyze(fixtureBatch(start))
	inv := model.Investigation{ID: "inv-x", Name: "缺失", RootID: "nope", TraceID: "trace"}
	if _, err := Compare(result, result, inv, inv); err == nil {
		t.Fatal("expected error for missing root")
	}
}
