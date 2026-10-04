package review

import (
	"testing"
	"time"

	"performance-investigation/internal/analysis"
	"performance-investigation/internal/model"
)

func batchFixture(records ...model.CallRecord) model.Batch {
	start := time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC)
	if len(records) == 0 {
		records = []model.CallRecord{
			rec("root", "", "a", start, start.Add(time.Second)),
			rec("remote", "root", "b", start.Add(200*time.Millisecond), start.Add(800*time.Millisecond)),
		}
	}
	return model.Batch{ID: "batch-x", Source: "manual", Format: "manual-calls-v1", ImportedAt: start, Records: records}
}

func rec(id, parent, device string, start, end time.Time) model.CallRecord {
	return model.CallRecord{ID: id, TraceID: "trace", ParentID: parent, Service: "svc", Operation: "op", DeviceID: device, Start: start, End: end,
		DurationMS: model.DurationBreakdown{Nominal: float64(end.Sub(start).Microseconds()) / 1000, Certain: true}}
}

func offsetCons(id, anchor, target string, min, max float64) model.ClockConstraint {
	return model.ClockConstraint{ID: id, Type: model.ConstraintOffset, Anchor: anchor, Target: target, MinMS: min, MaxMS: max}
}

func inv(id string, constraints ...model.ClockConstraint) model.Investigation {
	return model.Investigation{ID: id, Name: id, BatchID: "batch-x", RootID: "root", TraceID: "trace",
		Hypothesis: model.ClockHypothesis{Name: "h-" + id, Constraints: constraints}}
}

func buildPackage(t *testing.T, batch model.Batch, invs []model.Investigation, withComparison bool) model.ReviewPackage {
	t.Helper()
	results := make([]model.Analysis, 0, len(invs))
	for _, item := range invs {
		results = append(results, analysis.AnalyzeWithClock(batch, item.Hypothesis))
	}
	var comparison *model.Comparison
	if withComparison && len(invs) == 2 {
		c, err := analysis.Compare(results[0], results[1], invs[0], invs[1])
		if err != nil {
			t.Fatal(err)
		}
		comparison = &c
	}
	return Build(batch, invs, results, comparison, time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC))
}

func issueTypes(issues []model.ReviewIssue) map[string]int {
	out := map[string]int{}
	for _, issue := range issues {
		out[issue.Type]++
	}
	return out
}

// 互斥约束：封存时不可行与冲突链必须随包往返，重算仍不可行且结论一致。
func TestContradictoryConstraintsRoundTrip(t *testing.T) {
	batch := batchFixture()
	invs := []model.Investigation{inv("inv-mutex",
		offsetCons("c1", "root", "remote", 150, 250),
		offsetCons("c2", "root", "remote", 400, 500))}
	pkg := buildPackage(t, batch, invs, false)
	if len(pkg.Saved) != 1 || pkg.Saved[0].Clock.Feasible {
		t.Fatalf("saved conclusion must record infeasibility, got %+v", pkg.Saved)
	}
	savedChain := pkg.Saved[0].Clock.Conflicts
	if len(savedChain) == 0 || len(savedChain[0].ConstraintIDs) != 2 {
		t.Fatalf("conflict chain must be sealed in package, got %+v", savedChain)
	}
	summary, issues := Recompute(pkg)
	if !summary.ConclusionMatch {
		t.Fatalf("same inputs on same version must reproduce the infeasible conclusion exactly: %v", summary.Notes)
	}
	if got := issueTypes(issues)["contradiction"]; got != 1 {
		t.Fatalf("recompute must keep contradiction in issue list, got %v (%+v)", got, issues)
	}
	if summary.Entries[0].Clock.Feasible {
		t.Fatal("recomputed result must remain infeasible")
	}
}

// 停用互斥依据后可行：停用状态随包往返，重算只采用启用依据。
func TestDisabledConstraintBecomesFeasible(t *testing.T) {
	batch := batchFixture()
	disabled := offsetCons("c2", "root", "remote", 400, 500)
	disabled.Disabled = true
	invs := []model.Investigation{inv("inv-disabled", offsetCons("c1", "root", "remote", 150, 250), disabled)}
	pkg := buildPackage(t, batch, invs, false)
	summary, issues := Recompute(pkg)
	if len(issues) != 0 {
		t.Fatalf("disabled conflicting constraint must recompute cleanly, got %+v", issues)
	}
	if !summary.ConclusionMatch {
		t.Fatalf("saved and recomputed conclusions must match: %v", summary.Notes)
	}
	entry := summary.Entries[0]
	if !entry.Clock.Feasible || len(entry.Clock.DisabledIDs) != 1 || entry.Clock.DisabledIDs[0] != "c2" {
		t.Fatalf("c2 must stay disabled and excluded from solve, got %+v", entry.Clock)
	}
	if len(entry.Clock.ConstraintIDs) != 1 || entry.Clock.ConstraintIDs[0] != "c1" {
		t.Fatalf("only enabled c1 must be applied, got %v", entry.Clock.ConstraintIDs)
	}
	if got := entry.RootTiming.WaitMS.Upper; got != 700 {
		t.Fatalf("wait upper = %v, want 700 from enabled c1", got)
	}
}

// 相同原始资料、不同假设的往返：两侧结论与对照结论都应重算一致。
func TestSameRawDifferentHypothesesRoundTrip(t *testing.T) {
	batch := batchFixture()
	invs := []model.Investigation{
		inv("inv-wide"),
		inv("inv-tight", offsetCons("c1", "root", "remote", 150, 250)),
	}
	pkg := buildPackage(t, batch, invs, true)
	if pkg.Comparison == nil || !pkg.Comparison.AssumptionChange {
		t.Fatalf("sealed comparison must flag assumption change, got %+v", pkg.Comparison)
	}
	summary, issues := Recompute(pkg)
	if len(issues) != 0 {
		t.Fatalf("clean package must recompute without issues, got %+v", issues)
	}
	if !summary.ConclusionMatch || summary.ComparisonMatch == nil || !*summary.ComparisonMatch {
		t.Fatalf("both conclusions and comparison must reproduce on another instance: %+v %v", summary.Notes, summary.ComparisonMatch)
	}
	if !summary.Entries[1].RootTiming.AssumptionBased {
		t.Fatal("tight hypothesis must re-tighten bounds on recompute")
	}
}

// 约束引用缺失记录：依据保留在包内与问题清单中，不被默默丢弃。
func TestMissingRecordKeepsBasisAndIssue(t *testing.T) {
	batch := batchFixture()
	invs := []model.Investigation{inv("inv-missing", offsetCons("c1", "root", "ghost", 100, 200))}
	pkg := buildPackage(t, batch, invs, false)
	_, issues := Recompute(pkg)
	types := issueTypes(issues)
	if types["missing_record"] == 0 || types["unknown_record"] == 0 {
		t.Fatalf("missing record must appear in issue list from audit and solver, got %+v", issues)
	}
	var listed *model.ReviewIssue
	for i := range issues {
		if issues[i].Type == "missing_record" {
			listed = &issues[i]
		}
	}
	if listed == nil || len(listed.ConstraintIDs) != 1 || listed.ConstraintIDs[0] != "c1" {
		t.Fatalf("issue must retain the basis c1, got %+v", listed)
	}
	if !containsID(listed.RecordIDs, "ghost") {
		t.Fatalf("issue must name missing record ghost, got %v", listed.RecordIDs)
	}
	found := false
	for _, c := range pkg.Investigations[0].Hypothesis.Constraints {
		if c.ID == "c1" {
			found = true
		}
	}
	if !found {
		t.Fatal("constraint c1 must remain in the package hypothesis")
	}
}

// 重复 ID：全部原始副本保留，问题清单显式标出重复，不静默删除。
func TestDuplicateIDsAreReportedNotDropped(t *testing.T) {
	start := time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC)
	dup := rec("remote", "root", "b", start.Add(200*time.Millisecond), start.Add(800*time.Millisecond))
	batch := batchFixture(
		rec("root", "", "a", start, start.Add(time.Second)),
		dup,
		dup,
	)
	invs := []model.Investigation{inv("inv-dup", offsetCons("c1", "root", "remote", 100, 200))}
	pkg := buildPackage(t, batch, invs, false)
	rawCount := 0
	for _, r := range pkg.Batch.Records {
		if r.ID == "remote" {
			rawCount++
		}
	}
	if rawCount != 2 {
		t.Fatalf("both duplicate raw records must be exported, got %d", rawCount)
	}
	_, issues := Recompute(pkg)
	types := issueTypes(issues)
	if types["duplicate_id"] == 0 {
		t.Fatalf("duplicate id must be reported, got %+v", issues)
	}
	if types["missing_record"] != 0 || types["unknown_record"] != 0 {
		t.Fatalf("duplicate-but-present id must not be treated as missing, got %+v", issues)
	}
}

func containsID(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
