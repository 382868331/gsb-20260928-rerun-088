package analysis

import (
	"fmt"
	"time"

	"performance-investigation/internal/model"
)

// BuildReviewBundle 把调查、原始批次、启用与停用的时钟依据、冲突链
// 以及可选的调查对照结论打包为可导出的复核包；结论快照取自导出时刻
// 按原假设求解的结果，原始记录与时间戳不被修改。
func BuildReviewBundle(inv model.Investigation, batch model.Batch, related []model.Investigation, comparison *model.Comparison) model.ReviewBundle {
	result := AnalyzeWithClock(batch, inv.Hypothesis)
	return model.ReviewBundle{
		Format:                model.ReviewBundleFormat,
		ExportedAt:            time.Now().UTC(),
		Investigation:         inv,
		Batch:                 batch,
		RelatedInvestigations: related,
		SavedConclusions: model.SavedConclusions{
			Clock:    result.Clock,
			Requests: result.Requests,
		},
		Comparison: comparison,
	}
}

// ValidateReviewBundle 检查复核包完整性：约束引用的记录缺失、记录或约束
// ID 重复都会列入问题清单；问题只记录不丢弃，原始依据全部保留。
func ValidateReviewBundle(bundle model.ReviewBundle) []model.ReviewProblem {
	problems := []model.ReviewProblem{}
	if bundle.Investigation.BatchID != "" && bundle.Batch.ID != "" && bundle.Investigation.BatchID != bundle.Batch.ID {
		problems = append(problems, model.ReviewProblem{
			Type:    model.ProblemBatchMismatch,
			ID:      bundle.Investigation.ID,
			Summary: fmt.Sprintf("调查 %s 引用批次 %s，但复核包内批次为 %s，已按包内批次重算", bundle.Investigation.ID, bundle.Investigation.BatchID, bundle.Batch.ID),
		})
	}
	recordCount := map[string]int{}
	for _, rec := range bundle.Batch.Records {
		recordCount[rec.ID]++
	}
	for id, count := range recordCount {
		if count > 1 {
			problems = append(problems, model.ReviewProblem{
				Type:    model.ProblemDuplicateRecordID,
				ID:      id,
				Summary: fmt.Sprintf("记录 ID %s 出现 %d 次，全部保留为原始证据，重复副本不参与耗时求和", id, count),
			})
		}
	}
	constraintIDs := map[string]bool{}
	for _, constraint := range bundle.Investigation.Hypothesis.Constraints {
		if constraintIDs[constraint.ID] {
			problems = append(problems, model.ReviewProblem{
				Type:    model.ProblemDuplicateConstraintID,
				ID:      constraint.ID,
				Summary: fmt.Sprintf("时钟依据 ID %s 重复出现，两条依据都保留并参与求解", constraint.ID),
			})
		}
		constraintIDs[constraint.ID] = true
		for _, ref := range constraintRecordRefs(constraint) {
			if recordCount[ref] == 0 {
				problems = append(problems, model.ReviewProblem{
					Type:    model.ProblemMissingRecord,
					ID:      constraint.ID,
					Summary: fmt.Sprintf("时钟依据 %s 引用的记录 %s 不在复核包中，依据保留但求解时会标记为未知记录", constraint.ID, ref),
				})
			}
		}
	}
	return problems
}

// constraintRecordRefs 提取约束端点引用的原始记录 ID。
func constraintRecordRefs(constraint model.ClockConstraint) []string {
	refs := []string{}
	for _, endpoint := range []string{constraint.Before, constraint.After, constraint.Anchor, constraint.Target} {
		if endpoint == "" {
			continue
		}
		refs = append(refs, recordOfRef(endpoint))
	}
	return refs
}
