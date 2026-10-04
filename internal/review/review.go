// Package review 构建与解析可导出的调查复核包。
// 复核包封存原始记录、启用/停用时钟依据、冲突链与调查对照结论；
// 导入方按包内原依据重新求解，并把原保存结论与重新求解结果分开呈现。
package review

import (
	"fmt"
	"time"

	"performance-investigation/internal/analysis"
	"performance-investigation/internal/model"
)

// Build 用批次原始记录、调查及其对照结论组装复核包。
// conclusions 与 comparison 在导出当时计算并封存，导入方不会把它们当作重算结果。
func Build(batch model.Batch, investigations []model.Investigation, conclusions []model.Analysis, comparison *model.Comparison, exportedAt time.Time) model.ReviewPackage {
	pkg := model.ReviewPackage{
		Format:         model.ReviewPackageFormat,
		ExportedAt:     exportedAt.UTC(),
		Batch:          batch,
		Investigations: append([]model.Investigation(nil), investigations...),
		Comparison:     comparison,
	}
	for index, result := range conclusions {
		inv := investigations[index]
		root := analysis.RootNodeFor(result, inv)
		conclusion := model.ReviewSavedConclusion{
			InvestigationID: inv.ID,
			Name:            inv.Name,
			RootID:          inv.RootID,
			TraceID:         inv.TraceID,
			Clock:           result.Clock,
			Anomalies:       result.Anomalies,
			Notes:           inv.Notes,
			SavedAt:         exportedAt.UTC(),
		}
		if root != nil {
			conclusion.RootTiming = root.Timing
		}
		pkg.Saved = append(pkg.Saved, conclusion)
	}
	return pkg
}

// Recompute 在导入侧用包内原始记录与时钟依据重新求解，
// 返回重算结果以及问题清单；任何缺失记录、重复 ID、失效依据都显式保留。
func Recompute(pkg model.ReviewPackage) (model.ReviewRecomputeSummary, []model.ReviewIssue) {
	issues := audit(pkg)
	summary := model.ReviewRecomputeSummary{Entries: []model.ReviewRecomputedEntry{}, ConclusionMatch: true}
	recomputed := make(map[string]model.Analysis, len(pkg.Investigations))
	savedByID := map[string]model.ReviewSavedConclusion{}
	for _, saved := range pkg.Saved {
		savedByID[saved.InvestigationID] = saved
	}
	for _, inv := range pkg.Investigations {
		result := analysis.AnalyzeWithClock(pkg.Batch, inv.Hypothesis)
		recomputed[inv.ID] = result
		entry := model.ReviewRecomputedEntry{InvestigationID: inv.ID, Found: true, Clock: result.Clock}
		if root := analysis.RootNodeFor(result, inv); root != nil {
			entry.RootTiming = root.Timing
		}
		// 约束求解自身发现的问题（引用缺失/重复记录、无效依据、矛盾链）同样并入问题清单。
		for _, conflict := range result.Clock.Conflicts {
			issues = append(issues, model.ReviewIssue{
				Severity:      model.ReviewIssueError,
				Type:          conflict.Type,
				ConstraintIDs: append([]string(nil), conflict.ConstraintIDs...),
				RecordIDs:     append([]string(nil), conflict.RecordIDs...),
				Investigation: inv.ID,
				Summary:       conflict.Summary,
			})
		}
		saved, hasSaved := savedByID[inv.ID]
		if !hasSaved {
			summary.ConclusionMatch = false
			summary.Notes = append(summary.Notes, fmt.Sprintf("调查 %s 在复核包中没有封存结论，无法对照原保存结果", inv.ID))
		} else if !timingEqual(saved.RootTiming, entry.RootTiming) || !clockEqual(saved.Clock, entry.Clock) {
			summary.ConclusionMatch = false
			summary.Notes = append(summary.Notes, fmt.Sprintf("调查 %s 重新求解结果与原保存结论不一致：区间或可行性已变化，请核对原始记录与时钟依据", inv.ID))
		}
		summary.Entries = append(summary.Entries, entry)
	}
	for _, saved := range pkg.Saved {
		if _, ok := recomputed[saved.InvestigationID]; !ok {
			summary.ConclusionMatch = false
			summary.Notes = append(summary.Notes, fmt.Sprintf("封存结论属于调查 %s，但调查本体缺失，无法重新求解", saved.InvestigationID))
		}
	}
	if pkg.Comparison != nil && len(pkg.Investigations) >= 2 {
		invA, invB := pkg.Investigations[0], pkg.Investigations[1]
		comparison, err := analysis.Compare(recomputed[invA.ID], recomputed[invB.ID], invA, invB)
		if err != nil {
			issues = append(issues, model.ReviewIssue{Severity: model.ReviewIssueError, Type: "comparison_failed", Summary: err.Error()})
		} else {
			match := comparisonEqual(*pkg.Comparison, comparison)
			summary.ComparisonMatch = &match
			if !match {
				summary.ConclusionMatch = false
				summary.Notes = append(summary.Notes, "重新求解的调查对照结论与封存对照结论不一致")
			}
		}
	}
	return summary, dedupeIssues(issues)
}

// audit 检查复核包自身的数据完整性：重复记录 ID、约束引用缺失记录等。
func audit(pkg model.ReviewPackage) []model.ReviewIssue {
	issues := []model.ReviewIssue{}
	counts := map[string]int{}
	for _, record := range pkg.Batch.Records {
		counts[record.ID]++
	}
	duplicates := []string{}
	for id, count := range counts {
		if count > 1 {
			duplicates = append(duplicates, id)
			issues = append(issues, model.ReviewIssue{
				Severity:  model.ReviewIssueError,
				Type:      "duplicate_id",
				RecordIDs: []string{id},
				Summary:   fmt.Sprintf("原始记录 ID %s 出现 %d 次：全部保留在问题清单中，求解只采用首次代表，依据不会被默默丢弃", id, count),
			})
		}
	}
	for _, inv := range pkg.Investigations {
		for _, constraint := range inv.Hypothesis.Constraints {
			missing := missingEndpointRecords(constraint, counts)
			if len(missing) > 0 {
				issues = append(issues, model.ReviewIssue{
					Severity:      model.ReviewIssueError,
					Type:          "missing_record",
					ConstraintIDs: []string{constraint.ID},
					RecordIDs:     missing,
					Investigation: inv.ID,
					Summary:       fmt.Sprintf("约束 %s 引用的原始记录 %v 不在批次中：依据保留并标记，不参与求解", constraint.ID, missing),
				})
			}
			if constraint.MaxMS < constraint.MinMS {
				issues = append(issues, model.ReviewIssue{
					Severity:      model.ReviewIssueError,
					Type:          "invalid_range",
					ConstraintIDs: []string{constraint.ID},
					Investigation: inv.ID,
					Summary:       fmt.Sprintf("约束 %s 的时差范围 %v..%v 不成立", constraint.ID, constraint.MinMS, constraint.MaxMS),
				})
			}
		}
	}
	return issues
}

func missingEndpointRecords(constraint model.ClockConstraint, counts map[string]int) []string {
	ids := []string{}
	switch constraint.Type {
	case model.ConstraintBefore:
		ids = []string{constraint.Before, constraint.After}
	case model.ConstraintOffset:
		ids = []string{constraint.Anchor, constraint.Target}
	default:
		return nil
	}
	missing := []string{}
	seen := map[string]bool{}
	for _, ref := range ids {
		id := recordID(ref)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		if counts[id] == 0 {
			missing = append(missing, id)
		}
	}
	return missing
}

// recordID 去掉可能的 :start/:end 事件后缀。
func recordID(ref string) string {
	for i := len(ref) - 1; i >= 0; i-- {
		if ref[i] == ':' {
			return ref[:i]
		}
	}
	return ref
}

func timingEqual(a, b model.TimingSplit) bool {
	return breakdownEqual(a.WaitMS, b.WaitMS) && breakdownEqual(a.ExecutionMS, b.ExecutionMS) &&
		a.BoundsCertain == b.BoundsCertain && a.AssumptionBased == b.AssumptionBased
}

func breakdownEqual(a, b model.DurationBreakdown) bool {
	return near(a.Nominal, b.Nominal) && near(a.Lower, b.Lower) && near(a.Upper, b.Upper) && a.Certain == b.Certain
}

func clockEqual(a, b model.ClockReport) bool {
	if a.Feasible != b.Feasible || !sameSet(a.ConstraintIDs, b.ConstraintIDs) || !sameSet(a.DisabledIDs, b.DisabledIDs) {
		return false
	}
	if len(a.Conflicts) != len(b.Conflicts) {
		return false
	}
	for index := range a.Conflicts {
		if a.Conflicts[index].Type != b.Conflicts[index].Type ||
			!sameSet(a.Conflicts[index].ConstraintIDs, b.Conflicts[index].ConstraintIDs) ||
			!sameSet(a.Conflicts[index].RecordIDs, b.Conflicts[index].RecordIDs) {
			return false
		}
	}
	return sameSet(a.UncertainRecordIDs, b.UncertainRecordIDs)
}

func comparisonEqual(a, b model.Comparison) bool {
	return near(a.DurationDeltaMS, b.DurationDeltaMS) && breakdownEqual(a.ExecutionDelta, b.ExecutionDelta) &&
		breakdownEqual(a.WaitDelta, b.WaitDelta) && a.Attribution == b.Attribution &&
		a.AssumptionChange == b.AssumptionChange && a.EvidenceIncomplete == b.EvidenceIncomplete
}

func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	set := map[string]int{}
	for _, item := range a {
		set[item]++
	}
	for _, item := range b {
		set[item]--
		if set[item] < 0 {
			return false
		}
	}
	return true
}

func near(a, b float64) bool {
	diff := a - b
	if diff < 0 {
		diff = -diff
	}
	return diff < 1e-6
}

func dedupeIssues(issues []model.ReviewIssue) []model.ReviewIssue {
	seen := map[string]bool{}
	out := []model.ReviewIssue{}
	for _, issue := range issues {
		key := fmt.Sprintf("%s|%s|%s|%v|%v", issue.Type, issue.Investigation, issue.Summary, issue.ConstraintIDs, issue.RecordIDs)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, issue)
	}
	return out
}
