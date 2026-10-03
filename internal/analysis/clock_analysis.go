package analysis

import (
	"sort"

	"performance-investigation/internal/model"
)

// AnalyzeWithClock 在原始记录分析之上应用研究者保存的时钟假设：
// 约束只收紧跨设备区间的可证明上下界，原始时间戳从不被修改；
// 假设收紧得到的区间会标记 AssumptionBased，与原始证据可证明的部分区分。
func AnalyzeWithClock(batch model.Batch, hypothesis model.ClockHypothesis) model.Analysis {
	result := analyzeRaw(batch)
	solver, conflicts := solveClock(result.Batch.Records, hypothesis.Constraints)
	report := model.ClockReport{
		Feasible:      solver.feasible,
		Conflicts:     conflicts,
		ConstraintIDs: solver.usedConstraints,
		DisabledIDs:   solver.disabledConstraints,
	}
	if len(report.ConstraintIDs) == 0 {
		report.ConstraintIDs = nil
	}
	if len(report.DisabledIDs) == 0 {
		report.DisabledIDs = nil
	}
	if len(report.Conflicts) == 0 {
		report.Conflicts = nil
	}
	uncertain := map[string]bool{}
	hasEnabled := false
	for _, constraint := range hypothesis.Constraints {
		if !constraint.Disabled {
			hasEnabled = true
			break
		}
	}
	for traceID, root := range result.Traces {
		if hasEnabled {
			refineNodeTiming(root, solver)
		}
		rootRef := eventRef(root.Record.ID, "start")
		walk(root, func(node *model.CallNode) {
			if node.Record.Duplicate || node.Record.ID == root.Record.ID {
				return
			}
			if _, _, ok := solver.offsetBounds(rootRef, eventRef(node.Record.ID, "start")); !ok {
				uncertain[node.Record.ID] = true
			}
		})
		result.Traces[traceID] = root
	}
	for id := range uncertain {
		report.UncertainRecordIDs = append(report.UncertainRecordIDs, id)
	}
	sort.Strings(report.UncertainRecordIDs)
	for index := range result.Requests {
		if root := result.Traces[result.Requests[index].TraceID]; root != nil {
			result.Requests[index].WaitMS = root.Timing.WaitMS.Nominal
			result.Requests[index].ExecutionMS = root.Timing.ExecutionMS.Nominal
			result.Requests[index].Certain = root.Timing.BoundsCertain
		}
	}
	result.Clock = report
	return result
}

// refineNodeTiming 用差分约束求解出的可证明事件区间重新计算等待/执行上下界。
// 同设备子调用区间精确；跨设备子调用只有在假设约束下才能得到有限包络，
// 此时区间被收紧但标记为假设推导，绝不伪装成原始证据可确定的值。
func refineNodeTiming(node *model.CallNode, solver *clockSolver) {
	for _, child := range node.Children {
		refineNodeTiming(child, solver)
	}
	duration := node.Timing.DurationMS
	if len(node.Children) == 0 || duration <= 0 {
		return
	}
	parentStart := eventRef(node.Record.ID, "start")
	type childBounds struct {
		outerStart, outerEnd float64 // 包络：子调用必然落在其中
		coreStart, coreEnd   float64 // 核心：必然被占用的部分
		nomStart, nomEnd     float64
		bounded              bool
		sameDevice           bool
		fallbackDuration     float64
	}
	bounds := []childBounds{}
	assumed := false
	for _, child := range node.Children {
		if child.Record.Duplicate || child.Record.End.Before(child.Record.Start) {
			continue
		}
		nomStart := msBetween(node.Record.Start, child.Record.Start)
		nomEnd := msBetween(node.Record.Start, child.Record.End)
		sameDevice := child.Record.DeviceID == node.Record.DeviceID
		item := childBounds{nomStart: nomStart, nomEnd: nomEnd, sameDevice: sameDevice}
		startLo, startHi, okStart := solver.offsetBounds(parentStart, eventRef(child.Record.ID, "start"))
		endLo, endHi, okEnd := solver.offsetBounds(parentStart, eventRef(child.Record.ID, "end"))
		if okStart && okEnd {
			item.bounded = true
			item.outerStart, item.outerEnd = startLo, endHi
			item.coreStart, item.coreEnd = startHi, endLo
			if !sameDevice {
				assumed = true
			}
		} else {
			item.outerStart, item.outerEnd = 0, duration
			item.fallbackDuration = nomEnd - nomStart
		}
		bounds = append(bounds, item)
	}
	if len(bounds) == 0 {
		return
	}
	// 等待下界：必然被占用的核心区间并集；无界子调用至少贡献其名义时长。
	coreIntervals := []interval{}
	waitLower := 0.0
	// 等待上界：外包络区间并集（并行重叠只计一次，不逐项相加）。
	outerIntervals := []interval{}
	nominalIntervals := []interval{}
	certain := true
	parallel := false
	unbounded := false
	for _, item := range bounds {
		if !item.sameDevice {
			certain = false
		}
		if item.bounded {
			if item.coreEnd > item.coreStart {
				coreIntervals = append(coreIntervals, interval{start: item.coreStart, end: item.coreEnd, certain: true})
			}
			outerIntervals = append(outerIntervals, interval{start: item.outerStart, end: item.outerEnd, certain: true})
		} else {
			unbounded = true
			if item.fallbackDuration > waitLower {
				waitLower = item.fallbackDuration
			}
		}
		nominalIntervals = append(nominalIntervals, interval{start: item.nomStart, end: item.nomEnd, certain: true})
	}
	if coreUnion := unionExact(coreIntervals); coreUnion > waitLower {
		waitLower = coreUnion
	}
	waitUpper := clamp(unionExact(outerIntervals), 0, duration)
	if unbounded {
		// 存在无法界定包络的跨设备子调用，等待上界只能保守取整段时长。
		waitUpper = duration
	}
	if waitLower > waitUpper {
		waitLower = waitUpper
	}
	waitNominal := clamp(unionExact(nominalIntervals), waitLower, waitUpper)
	if hasOverlap(outerIntervals) {
		parallel = true
	}
	reason := node.Timing.Reason
	if assumed {
		reason = "跨设备区间已按当前时钟假设收紧；上下界来自假设而非原始时间戳可直接证明；" + reason
	}
	node.Timing.WaitMS = model.DurationBreakdown{Nominal: waitNominal, Lower: waitLower, Upper: waitUpper, Certain: certain, Reason: reason}
	node.Timing.ExecutionMS = model.DurationBreakdown{Nominal: duration - waitNominal, Lower: duration - waitUpper, Upper: duration - waitLower, Certain: certain}
	node.Timing.BoundsCertain = certain
	node.Timing.AssumptionBased = assumed
	node.Timing.Parallel = parallel
	node.Timing.Reason = reason
}
