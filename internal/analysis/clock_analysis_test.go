package analysis

import (
	"testing"
	"time"

	"performance-investigation/internal/model"
)

func crossDeviceBatch() model.Batch {
	start := time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC)
	return model.Batch{Records: []model.CallRecord{
		record("root", "trace", "", "a", start, start.Add(time.Second)),
		record("remote", "trace", "root", "b", start.Add(200*time.Millisecond), start.Add(800*time.Millisecond)),
	}}
}

func crossDeviceBatchWithThirdDevice() model.Batch {
	batch := crossDeviceBatch()
	start := time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC)
	batch.Records = append(batch.Records, record("other", "trace", "root", "c", start.Add(100*time.Millisecond), start.Add(400*time.Millisecond)))
	return batch
}

func offsetConstraint(id, anchor, target string, min, max float64) model.ClockConstraint {
	return model.ClockConstraint{ID: id, Type: model.ConstraintOffset, Anchor: anchor, Target: target, MinMS: min, MaxMS: max}
}

func TestClockHypothesisTightensCrossDeviceBounds(t *testing.T) {
	batch := crossDeviceBatch()
	baseline := AnalyzeWithClock(batch, model.ClockHypothesis{})
	root := baseline.Traces["trace"]
	if root.Timing.AssumptionBased {
		t.Fatal("no hypothesis must not mark assumption-based bounds")
	}
	hypothesis := model.ClockHypothesis{Name: "对时假设", Constraints: []model.ClockConstraint{
		offsetConstraint("c1", "root", "remote", 150, 250),
	}}
	result := AnalyzeWithClock(batch, hypothesis)
	tightened := result.Traces["trace"]
	if !tightened.Timing.AssumptionBased {
		t.Fatal("expected assumption-based flag once constraints tighten bounds")
	}
	if tightened.Timing.BoundsCertain {
		t.Fatal("assumption-tightened bounds must not be presented as certain")
	}
	if got := tightened.Timing.WaitMS.Upper; got != 700 {
		t.Fatalf("wait upper = %v, want 700 (envelope 150..850 union length)", got)
	}
	if got := tightened.Timing.WaitMS.Lower; got != 500 {
		t.Fatalf("wait lower = %v, want 500 (guaranteed core 250..750)", got)
	}
	if result.Clock.Feasible != true || len(result.Clock.Conflicts) != 0 {
		t.Fatalf("single consistent hypothesis must be feasible: %+v", result.Clock)
	}
	// 未纳入任何约束的 other（设备 c）仍然无法确定，不得伪装成确定排名。
	withThird := AnalyzeWithClock(crossDeviceBatchWithThirdDevice(), hypothesis)
	found := false
	for _, id := range withThird.Clock.UncertainRecordIDs {
		if id == "other" {
			found = true
		}
	}
	if !found {
		t.Fatalf("unconstrained cross-device record must stay uncertain, got %v", withThird.Clock.UncertainRecordIDs)
	}
	// 原始时间戳不被假设修改。
	for _, rec := range result.Batch.Records {
		if rec.ID == "remote" && rec.Start != batch.Records[1].Start {
			t.Fatal("hypothesis must never rewrite raw timestamps")
		}
	}
}

func TestConflictingConstraintsReportChainAndFallback(t *testing.T) {
	batch := crossDeviceBatch()
	hypothesis := model.ClockHypothesis{Constraints: []model.ClockConstraint{
		offsetConstraint("c1", "root", "remote", 150, 250),
		offsetConstraint("c2", "root", "remote", 400, 500),
	}}
	result := AnalyzeWithClock(batch, hypothesis)
	if result.Clock.Feasible {
		t.Fatal("contradictory offset ranges must be infeasible")
	}
	var conflict *model.ClockConflict
	for index := range result.Clock.Conflicts {
		if result.Clock.Conflicts[index].Type == "contradiction" {
			conflict = &result.Clock.Conflicts[index]
		}
	}
	if conflict == nil {
		t.Fatalf("expected contradiction conflict, got %+v", result.Clock.Conflicts)
	}
	joined := ""
	for _, id := range conflict.ConstraintIDs {
		joined += id + ";"
	}
	if joined != "c1;c2;" {
		t.Fatalf("conflict must name both constraints, got %v", conflict.ConstraintIDs)
	}
	hasRecord := map[string]bool{}
	for _, id := range conflict.RecordIDs {
		hasRecord[id] = true
	}
	if !hasRecord["root"] || !hasRecord["remote"] {
		t.Fatalf("conflict must locate involved records, got %v", conflict.RecordIDs)
	}
	// 矛盾时回退到仅设备时钟的区间，不采用任何矛盾假设。
	root := result.Traces["trace"]
	if root.Timing.AssumptionBased {
		t.Fatal("contradictory hypothesis must fall back without tightening")
	}
	if got := root.Timing.WaitMS.Upper; got != 1000 {
		t.Fatalf("fallback wait upper = %v, want 1000", got)
	}
}

func TestDisabledConstraintRestoresFeasibility(t *testing.T) {
	batch := crossDeviceBatch()
	disabled := offsetConstraint("c2", "root", "remote", 400, 500)
	disabled.Disabled = true
	hypothesis := model.ClockHypothesis{Constraints: []model.ClockConstraint{
		offsetConstraint("c1", "root", "remote", 150, 250),
		disabled,
	}}
	result := AnalyzeWithClock(batch, hypothesis)
	if !result.Clock.Feasible {
		t.Fatalf("disabled constraint must not break feasibility: %+v", result.Clock.Conflicts)
	}
	if len(result.Clock.DisabledIDs) != 1 || result.Clock.DisabledIDs[0] != "c2" {
		t.Fatalf("disabled constraint must be listed, got %v", result.Clock.DisabledIDs)
	}
	if got := result.Traces["trace"].Timing.WaitMS.Upper; got != 700 {
		t.Fatalf("wait upper = %v, want 700 from enabled constraint only", got)
	}
}

func TestCompareFlagsAssumptionOnlyChange(t *testing.T) {
	batch := crossDeviceBatch()
	invA := model.Investigation{ID: "a", Name: "宽假设", BatchID: "b", RootID: "root", TraceID: "trace"}
	invB := model.Investigation{ID: "b", Name: "紧假设", BatchID: "b", RootID: "root", TraceID: "trace",
		Hypothesis: model.ClockHypothesis{Constraints: []model.ClockConstraint{offsetConstraint("c1", "root", "remote", 150, 250)}}}
	comparison, err := Compare(AnalyzeWithClock(batch, invA.Hypothesis), AnalyzeWithClock(batch, invB.Hypothesis), invA, invB)
	if err != nil {
		t.Fatal(err)
	}
	if !comparison.AssumptionChange {
		t.Fatal("same batch with different hypotheses must flag assumption change")
	}
	if comparison.DurationDeltaMS != 0 {
		t.Fatalf("raw duration unchanged, delta = %v", comparison.DurationDeltaMS)
	}
	found := false
	for _, note := range comparison.Notes {
		if note == "总耗时未变，观察到的区间变化由假设收紧造成" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected assumption-tightening note, got %v", comparison.Notes)
	}
}
