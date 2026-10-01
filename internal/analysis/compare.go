package analysis

import (
	"fmt"
	"math"

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
