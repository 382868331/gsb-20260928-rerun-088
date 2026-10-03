package analysis

import (
	"testing"
	"time"

	"performance-investigation/internal/model"
)

func clockFixture(start time.Time) model.Batch {
	return model.Batch{Records: []model.CallRecord{
		record("root", "trace", "", "a", start, start.Add(time.Second)),
		record("local", "trace", "root", "a", start.Add(100*time.Millisecond), start.Add(400*time.Millisecond)),
		record("remote", "trace", "root", "b", start.Add(200*time.Millisecond), start.Add(800*time.Millisecond)),
	}}
}

func TestHypothesisTightensCrossDeviceOffsets(t *testing.T) {
	start := time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC)
	batch := clockFixture(start)

	plain := AnalyzeWithHypothesis(batch, model.ClockHypothesis{})
	if !plain.Clock.Feasible {
		t.Fatal("empty hypothesis must stay feasible")
	}
	remote := findOffset(plain.Clock.Offsets, "remote")
	if remote == nil || remote.Certain {
		t.Fatalf("cross-device record must stay uncertain without constraints, got %+v", remote)
	}

	hypothesis := model.ClockHypothesis{Name: "对时假设", Constraints: []model.ClockConstraint{
		{ID: "c1", Type: model.ConstraintOffset, Anchor: "root:start", Target: "remote:start", MinMS: 180, MaxMS: 260},
	}}
	solved := AnalyzeWithHypothesis(batch, hypothesis)
	if !solved.Clock.Feasible {
		t.Fatalf("consistent hypothesis must be feasible: %+v", solved.Clock.Conflicts)
	}
	remote = findOffset(solved.Clock.Offsets, "remote")
	if remote == nil || !remote.Certain {
		t.Fatalf("constraint should bound remote start, got %+v", remote)
	}
	if remote.StartLower < 180-1e-6 || remote.StartUpper > 260+1e-6 {
		t.Fatalf("remote start bounds = %v..%v, want within 180..260", remote.StartLower, remote.StartUpper)
	}
	if len(solved.Clock.ConstraintIDs) != 1 || solved.Clock.ConstraintIDs[0] != "c1" {
		t.Fatalf("active constraints = %v, want [c1]", solved.Clock.ConstraintIDs)
	}
}

func TestConflictingRangesReportRecordsAndConstraints(t *testing.T) {
	start := time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC)
	batch := clockFixture(start)
	hypothesis := model.ClockHypothesis{Constraints: []model.ClockConstraint{
		{ID: "c1", Type: model.ConstraintOffset, Anchor: "root:start", Target: "remote:start", MinMS: 100, MaxMS: 200},
		{ID: "c2", Type: model.ConstraintOffset, Anchor: "root:start", Target: "remote:start", MinMS: 500, MaxMS: 600},
	}}
	result := AnalyzeWithHypothesis(batch, hypothesis)
	if result.Clock.Feasible {
		t.Fatal("contradictory ranges must be infeasible")
	}
	var contradiction *model.ClockConflict
	for index := range result.Clock.Conflicts {
		if result.Clock.Conflicts[index].Type == "contradiction" {
			contradiction = &result.Clock.Conflicts[index]
		}
	}
	if contradiction == nil {
		t.Fatalf("expected contradiction conflict, got %+v", result.Clock.Conflicts)
	}
	if !containsAll(contradiction.ConstraintIDs, "c1", "c2") {
		t.Fatalf("conflict must name both constraints, got %v", contradiction.ConstraintIDs)
	}
	if !containsAll(contradiction.RecordIDs, "root", "remote") {
		t.Fatalf("conflict must name involved records, got %v", contradiction.RecordIDs)
	}

	// 停用其中一条约束后恢复可行，且停用的约束保留在报告中。
	hypothesis.Constraints[1].Disabled = true
	resolved := AnalyzeWithHypothesis(batch, hypothesis)
	if !resolved.Clock.Feasible {
		t.Fatalf("disabling one side must restore feasibility: %+v", resolved.Clock.Conflicts)
	}
	if len(resolved.Clock.InactiveConstraintIDs) != 1 || resolved.Clock.InactiveConstraintIDs[0] != "c2" {
		t.Fatalf("inactive constraints = %v, want [c2]", resolved.Clock.InactiveConstraintIDs)
	}
}

func TestDisabledRecordExcludedFromSolving(t *testing.T) {
	start := time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC)
	batch := clockFixture(start)
	hypothesis := model.ClockHypothesis{
		Constraints: []model.ClockConstraint{
			{ID: "c1", Type: model.ConstraintOffset, Anchor: "root:start", Target: "remote:start", MinMS: 100, MaxMS: 200},
		},
		DisabledRecords: []string{"remote"},
	}
	result := AnalyzeWithHypothesis(batch, hypothesis)
	if !containsAll(result.Clock.DisabledRecordIDs, "remote") {
		t.Fatalf("disabled records = %v, want [remote]", result.Clock.DisabledRecordIDs)
	}
	foundExcluded := false
	for _, conflict := range result.Clock.Conflicts {
		if conflict.Type == "excluded_record" && containsAll(conflict.ConstraintIDs, "c1") {
			foundExcluded = true
		}
	}
	if !foundExcluded {
		t.Fatalf("constraint on disabled record must be reported, got %+v", result.Clock.Conflicts)
	}
	if !containsAll(result.Clock.UncertainRecordIDs, "remote") {
		t.Fatalf("disabled record must be uncertain, got %v", result.Clock.UncertainRecordIDs)
	}
	// 恢复记录后约束重新生效。
	hypothesis.DisabledRecords = nil
	restored := AnalyzeWithHypothesis(batch, hypothesis)
	remote := findOffset(restored.Clock.Offsets, "remote")
	if remote == nil || !remote.Certain || remote.StartUpper > 200+1e-6 {
		t.Fatalf("restored record must be bounded by constraint, got %+v", remote)
	}
}

func TestCompareSameBatchDifferentHypothesisFlagsTightening(t *testing.T) {
	start := time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC)
	batch := clockFixture(start)
	invA := model.Investigation{ID: "inv-a", Name: "宽假设", BatchID: "b1", RootID: "root", TraceID: "trace"}
	invB := model.Investigation{ID: "inv-b", Name: "紧假设", BatchID: "b1", RootID: "root", TraceID: "trace", Hypothesis: model.ClockHypothesis{
		Name: "对时后",
		Constraints: []model.ClockConstraint{
			{ID: "c1", Type: model.ConstraintOffset, Anchor: "root:start", Target: "remote:start", MinMS: 180, MaxMS: 220},
		},
	}}
	comparison, err := Compare(AnalyzeWithHypothesis(batch, invA.Hypothesis), AnalyzeWithHypothesis(batch, invB.Hypothesis), invA, invB)
	if err != nil {
		t.Fatal(err)
	}
	if comparison.DurationDeltaMS != 0 {
		t.Fatalf("same batch must keep zero duration delta, got %v", comparison.DurationDeltaMS)
	}
	if !comparison.AssumptionTightened {
		t.Fatal("difference must be attributed to hypothesis tightening, not real change")
	}
	if comparison.Attribution != "hypothesis" {
		t.Fatalf("attribution = %q, want hypothesis", comparison.Attribution)
	}
	if comparison.HypothesisB != "对时后" {
		t.Fatalf("hypothesis names must be preserved, got %q", comparison.HypothesisB)
	}
}

func TestCompareDifferentBatchesNotTightening(t *testing.T) {
	start := time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC)
	slower := clockFixture(start)
	for index := range slower.Records {
		if slower.Records[index].ID == "root" {
			slower.Records[index].End = start.Add(2 * time.Second)
		}
	}
	invA := model.Investigation{ID: "inv-a", BatchID: "b1", RootID: "root", TraceID: "trace"}
	invB := model.Investigation{ID: "inv-b", BatchID: "b2", RootID: "root", TraceID: "trace"}
	comparison, err := Compare(Analyze(clockFixture(start)), Analyze(slower), invA, invB)
	if err != nil {
		t.Fatal(err)
	}
	if comparison.AssumptionTightened {
		t.Fatal("different batches must not be flagged as hypothesis tightening")
	}
}

func findOffset(offsets []model.OffsetBound, recordID string) *model.OffsetBound {
	for index := range offsets {
		if offsets[index].RecordID == recordID {
			return &offsets[index]
		}
	}
	return nil
}

func containsAll(values []string, wanted ...string) bool {
	set := map[string]bool{}
	for _, value := range values {
		set[value] = true
	}
	for _, item := range wanted {
		if !set[item] {
			return false
		}
	}
	return true
}
