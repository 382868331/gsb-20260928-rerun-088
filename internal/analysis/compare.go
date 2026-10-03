package analysis

import (
	"fmt"
	"math"
	"sort"

	"performance-investigation/internal/model"
)

// Compare 对照两次调查的根调用耗时，把总耗时增量拆分为本段执行、
// 等待（含并行合并后的下游等待）以及证据不完整导致的不确定区间。
func Compare(analysisA, analysisB model.Analysis, invA, invB model.Investigation) (model.Comparison, error) {
	sideA, err := side(analysisA, invA)
	if err != nil {
		return model.Comparison{}, err
	}
	sideB, err := side(analysisB, invB)
	if err != nil {
		return model.Comparison{}, err
	}
	comparison := model.Comparison{
		A:               sideA,
		B:               sideB,
		DurationDeltaMS: sideB.DurationMS - sideA.DurationMS,
		ExecutionDelta:  delta(sideA.ExecutionMS, sideB.ExecutionMS),
		WaitDelta:       delta(sideA.WaitMS, sideB.WaitMS),
		Notes:           []string{},
	}
	comparison.AssumptionChange = assumptionChange(invA, invB)
	comparison.Requests, comparison.RankingNotes = compareRequests(analysisA, analysisB)
	certain := comparison.ExecutionDelta.Certain && comparison.WaitDelta.Certain
	comparison.EvidenceIncomplete = !certain || sideA.AnomalyCount > 0 || sideB.AnomalyCount > 0
	comparison.Attribution = attribute(comparison)
	if !certain {
		comparison.Notes = append(comparison.Notes, "跨设备时钟无法确定先后顺序，增量以区间形式保留，未强行排序")
	}
	if sideA.Parallel || sideB.Parallel {
		comparison.Notes = append(comparison.Notes, "存在并行子调用，等待时间按区间并集合并，未逐项相加")
	}
	if sideA.AnomalyCount > 0 {
		comparison.Notes = append(comparison.Notes, fmt.Sprintf("调查 %s 含 %d 条异常记录（重复/缺父/时间倒置），证据不完整", sideA.Name, sideA.AnomalyCount))
	}
	if sideB.AnomalyCount > 0 {
		comparison.Notes = append(comparison.Notes, fmt.Sprintf("调查 %s 含 %d 条异常记录（重复/缺父/时间倒置），证据不完整", sideB.Name, sideB.AnomalyCount))
	}
	return comparison, nil
}

func side(result model.Analysis, inv model.Investigation) (model.ComparisonSide, error) {
	root := findRoot(result, inv)
	if root == nil {
		return model.ComparisonSide{}, fmt.Errorf("调查 %s 的根调用 %s 不在批次分析中", inv.ID, inv.RootID)
	}
	anomalyCount := 0
	walk(root, func(n *model.CallNode) {
		if len(n.Record.Anomalies) > 0 {
			anomalyCount++
		}
	})
	return model.ComparisonSide{
		InvestigationID: inv.ID,
		Name:            inv.Name,
		RootID:          root.Record.ID,
		TraceID:         root.Record.TraceID,
		DurationMS:      root.Timing.DurationMS,
		ExecutionMS:     root.Timing.ExecutionMS,
		WaitMS:          root.Timing.WaitMS,
		Certain:         root.Timing.BoundsCertain,
		Parallel:        root.Timing.Parallel,
		AnomalyCount:    anomalyCount,
	}, nil
}

func findRoot(result model.Analysis, inv model.Investigation) *model.CallNode {
	if trace := result.Traces[inv.TraceID]; trace != nil {
		if inv.RootID == "" || trace.Record.ID == inv.RootID {
			return trace
		}
		if found := findNode(trace, inv.RootID); found != nil {
			return found
		}
	}
	for _, trace := range result.Traces {
		if found := findNode(trace, inv.RootID); found != nil {
			return found
		}
	}
	return nil
}

func findNode(node *model.CallNode, id string) *model.CallNode {
	if node.Record.ID == id {
		return node
	}
	for _, child := range node.Children {
		if found := findNode(child, id); found != nil {
			return found
		}
	}
	return nil
}

// delta 计算 b-a；任一侧区间不确定时，结果保留可证明的上下界。
func delta(a, b model.DurationBreakdown) model.DurationBreakdown {
	result := model.DurationBreakdown{
		Nominal: b.Nominal - a.Nominal,
		Lower:   b.Lower - a.Upper,
		Upper:   b.Upper - a.Lower,
		Certain: a.Certain && b.Certain,
	}
	if result.Certain {
		result.Lower = result.Nominal
		result.Upper = result.Nominal
	}
	return result
}

func attribute(comparison model.Comparison) string {
	if !comparison.ExecutionDelta.Certain || !comparison.WaitDelta.Certain {
		return "uncertain"
	}
	exec := math.Abs(comparison.ExecutionDelta.Nominal)
	wait := math.Abs(comparison.WaitDelta.Nominal)
	switch {
	case exec == 0 && wait == 0:
		return "unchanged"
	case exec >= wait:
		return "execution"
	default:
		return "wait"
	}
}

// assumptionChange 说明两次调查采用的时钟依据是否一致，便于解读归因差异来源。
func assumptionChange(invA, invB model.Investigation) string {
	same := len(invA.Assumptions) == len(invB.Assumptions)
	if same {
		for index := range invA.Assumptions {
			if !constraintEqual(invA.Assumptions[index], invB.Assumptions[index]) {
				same = false
				break
			}
		}
	}
	if same {
		return "两次调查采用相同的时钟假设"
	}
	return fmt.Sprintf("时钟假设已调整：基线 %d 条约束，对照 %d 条约束；排名与等待比例需按区间重新判断", len(invA.Assumptions), len(invB.Assumptions))
}

func constraintEqual(a, b model.ClockConstraint) bool {
	if a.Type != b.Type || a.ARecordID != b.ARecordID || a.BRecordID != b.BRecordID {
		return false
	}
	if (a.LowerMS == nil) != (b.LowerMS == nil) || (a.UpperMS == nil) != (b.UpperMS == nil) {
		return false
	}
	if a.LowerMS != nil && *a.LowerMS != *b.LowerMS {
		return false
	}
	if a.UpperMS != nil && *a.UpperMS != *b.UpperMS {
		return false
	}
	return true
}

// compareRequests 对同一批次在两套时钟假设下的每个请求给出耗时与等待比例区间；
// 区间重叠时不判定先后，只保留“无法判定”的说明。
func compareRequests(analysisA, analysisB model.Analysis) ([]model.RequestComparison, []string) {
	requests := []model.RequestComparison{}
	notes := []string{}
	tracesA := rootNodes(analysisA)
	tracesB := rootNodes(analysisB)
	traceIDs := map[string]bool{}
	for id := range tracesA {
		traceIDs[id] = true
	}
	for id := range tracesB {
		traceIDs[id] = true
	}
	ordered := make([]string, 0, len(traceIDs))
	for id := range traceIDs {
		ordered = append(ordered, id)
	}
	sort.Strings(ordered)
	undecidable := map[string]bool{}
	for _, traceID := range ordered {
		nodeA, okA := tracesA[traceID]
		nodeB, okB := tracesB[traceID]
		if !okA || !okB {
			continue
		}
		item := model.RequestComparison{
			TraceID:    traceID,
			RootID:     nodeA.Record.ID,
			Service:    nodeA.Record.Service,
			Operation:  nodeA.Record.Operation,
			DurationA:  durationOf(nodeA),
			DurationB:  durationOf(nodeB),
			WaitShareA: waitShare(nodeA),
			WaitShareB: waitShare(nodeB),
		}
		requests = append(requests, item)
	}
	notes = append(notes, overlapNotes("基线", requests, func(r model.RequestComparison) model.DurationBreakdown { return r.WaitShareA }, undecidable)...)
	notes = append(notes, overlapNotes("对照", requests, func(r model.RequestComparison) model.DurationBreakdown { return r.WaitShareB }, undecidable)...)
	for index := range requests {
		if undecidable[requests[index].TraceID] {
			requests[index].OrderUndecidable = true
		}
	}
	return requests, notes
}

// overlapNotes 找出等待比例区间两两重叠的请求对；重叠即无法判定先后。
func overlapNotes(label string, requests []model.RequestComparison, pick func(model.RequestComparison) model.DurationBreakdown, undecidable map[string]bool) []string {
	notes := []string{}
	for i := range requests {
		for j := i + 1; j < len(requests); j++ {
			a, b := pick(requests[i]), pick(requests[j])
			if !a.Known || !b.Known {
				continue
			}
			if a.Lower <= b.Upper && b.Lower <= a.Upper {
				undecidable[requests[i].TraceID] = true
				undecidable[requests[j].TraceID] = true
				notes = append(notes, fmt.Sprintf("%s假设下 %s 与 %s 的等待比例区间重叠（%.0f%%–%.0f%% 与 %.0f%%–%.0f%%），无法判定先后",
					label, requests[i].Service, requests[j].Service, a.Lower*100, a.Upper*100, b.Lower*100, b.Upper*100))
			}
		}
	}
	return notes
}

func rootNodes(result model.Analysis) map[string]*model.CallNode {
	out := map[string]*model.CallNode{}
	for traceID, node := range result.Traces {
		out[traceID] = node
	}
	return out
}

func durationOf(node *model.CallNode) model.DurationBreakdown {
	duration := node.Timing.DurationMS
	return model.DurationBreakdown{Nominal: duration, Lower: duration, Upper: duration, Certain: true, Known: true}
}

func waitShare(node *model.CallNode) model.DurationBreakdown {
	duration := node.Timing.DurationMS
	if duration <= 0 {
		return model.DurationBreakdown{Known: false, Reason: "总耗时非正，无法计算等待比例"}
	}
	wait := node.Timing.WaitMS
	return model.DurationBreakdown{
		Nominal: wait.Nominal / duration,
		Lower:   wait.Lower / duration,
		Upper:   wait.Upper / duration,
		Certain: wait.Certain,
		Known:   true,
	}
}
