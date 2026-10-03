package analysis

import (
	"fmt"
	"math"
	"sort"
	"time"

	"performance-investigation/internal/model"
)

type interval struct {
	start, end float64
	certain    bool
	device     string
}

func Analyze(batch model.Batch) model.Analysis { return AnalyzeWithConstraints(batch, nil) }

// AnalyzeWithConstraints 在原始记录之上应用研究者补充的时钟约束重新推算。
// 原始时间戳保持不变；约束只影响区间推算，矛盾时以异常形式指出涉及的原记录。
func AnalyzeWithConstraints(batch model.Batch, constraints []model.ClockConstraint) model.Analysis {
	records := cloneRecords(batch.Records)
	byID := map[string]model.CallRecord{}
	duplicates := map[string]bool{}
	seenKey := map[string]string{}
	anomalies := []model.Anomaly{}
	traceRecords := map[string][]model.CallRecord{}

	for index := range records {
		rec := &records[index]
		key := rec.ID
		if key == "" {
			key = rec.TraceID + "|" + rec.ParentID + "|" + rec.Service + "|" + rec.Operation + "|" + rec.Start.Format(time.RFC3339Nano)
		}
		if first, ok := seenKey[key]; ok {
			duplicates[rec.ID] = true
			anomalies = append(anomalies, model.Anomaly{Type: "duplicate_record", RecordID: rec.ID, TraceID: rec.TraceID, Summary: fmt.Sprintf("记录 %s 与 %s 重复，已保留原始证据但不参与耗时求和", rec.ID, first)})
			continue
		}
		seenKey[key] = rec.ID
		if rec.ID != "" {
			byID[rec.ID] = *rec
		}
	}

	for index := range records {
		rec := &records[index]
		if duplicates[rec.ID] {
			rec.Duplicate = true
			rec.Anomalies = appendUnique(rec.Anomalies, "duplicate_record")
		}
		if rec.End.Before(rec.Start) {
			rec.Anomalies = appendUnique(rec.Anomalies, "negative_duration")
			anomalies = append(anomalies, model.Anomaly{Type: "negative_duration", RecordID: rec.ID, TraceID: rec.TraceID, Summary: "结束时间早于开始时间，原始时间不参与该记录耗时计算"})
		}
		if rec.ParentID != "" {
			if parent, ok := byID[rec.ParentID]; !ok || parent.TraceID != rec.TraceID {
				rec.Anomalies = appendUnique(rec.Anomalies, "missing_parent")
				anomalies = append(anomalies, model.Anomaly{Type: "missing_parent", RecordID: rec.ID, TraceID: rec.TraceID, Summary: fmt.Sprintf("父调用 %s 不在本批次中，保留为悬挂根节点", rec.ParentID)})
			}
		}
		if !rec.Duplicate {
			byID[rec.ID] = *rec
		}
	}

	for _, rec := range byID {
		traceRecords[rec.TraceID] = append(traceRecords[rec.TraceID], rec)
	}

	clock := buildClockModel(records, constraints)
	anomalies = append(anomalies, clock.anomalies...)
	conflictRecords := map[string]bool{}
	for _, anomaly := range clock.anomalies {
		if anomaly.Type != "constraint_conflict" {
			continue
		}
		for _, id := range anomaly.RecordIDs {
			conflictRecords[id] = true
		}
	}
	if len(conflictRecords) > 0 {
		for index := range records {
			if conflictRecords[records[index].ID] {
				records[index].Anomalies = appendUnique(records[index].Anomalies, "constraint_conflict")
				byID[records[index].ID] = records[index]
			}
		}
		traceRecords = map[string][]model.CallRecord{}
		for _, rec := range byID {
			traceRecords[rec.TraceID] = append(traceRecords[rec.TraceID], rec)
		}
	}

	traces := map[string]*model.CallNode{}
	summaries := []model.RequestSummary{}
	for traceID, list := range traceRecords {
		children := map[string][]*model.CallNode{}
		nodes := map[string]*model.CallNode{}
		var roots []*model.CallNode
		for _, rec := range list {
			nodes[rec.ID] = &model.CallNode{Record: rec, Level: 0}
		}
		for _, node := range nodes {
			parentID := node.Record.ParentID
			if parentID != "" {
				if parent := nodes[parentID]; parent != nil {
					children[parentID] = append(children[parentID], node)
					continue
				}
			}
			roots = append(roots, node)
		}
		sort.Slice(roots, func(i, j int) bool { return roots[i].Record.Start.Before(roots[j].Record.Start) })
		var root *model.CallNode
		if len(roots) == 1 {
			root = roots[0]
		} else if len(roots) > 1 {
			root = roots[0]
			for _, extra := range roots[1:] {
				anomalies = append(anomalies, model.Anomaly{Type: "multiple_roots", RecordID: extra.Record.ID, TraceID: traceID, Summary: fmt.Sprintf("调用 %s 缺少可验证父级，作为独立根展示", extra.Record.ID)})
			}
		} else {
			continue
		}
		assignChildren(root, children, 0)
		calculateTiming(root, clock)
		assignAbsoluteBounds(root, clock)
		traces[traceID] = root
		summaries = append(summaries, summarize(root, len(list)))
	}

	sort.Slice(summaries, func(i, j int) bool { return summaries[i].Start.After(summaries[j].Start) })
	sort.Slice(anomalies, func(i, j int) bool {
		if anomalies[i].TraceID == anomalies[j].TraceID {
			return anomalies[i].RecordID < anomalies[j].RecordID
		}
		return anomalies[i].TraceID < anomalies[j].TraceID
	})
	batch.Records = records
	return model.Analysis{Batch: batch, Requests: summaries, Traces: traces, Anomalies: anomalies, GeneratedAt: time.Now().UTC()}
}

func assignChildren(node *model.CallNode, children map[string][]*model.CallNode, level int) {
	node.Level = level
	kids := children[node.Record.ID]
	if kids == nil {
		kids = []*model.CallNode{}
	}
	sort.Slice(kids, func(i, j int) bool { return kids[i].Record.Start.Before(kids[j].Record.Start) })
	node.Children = kids
	for _, child := range kids {
		assignChildren(child, children, level+1)
	}
}

// placement 描述一个子调用相对父调用开始时刻的可能位置。
// definite 是任何符合约束的时钟取值下都被覆盖的部分；possible 是可能被覆盖的部分。
type placement struct {
	certain            bool
	unbounded          bool
	defStart, defEnd   float64
	hasDef             bool
	possStart, possEnd float64
	rawStart, rawEnd   float64
}

func calculateTiming(node *model.CallNode, clock clockModel) {
	duration := msBetween(node.Record.Start, node.Record.End)
	placements := []placement{}
	constrained := false
	for _, child := range node.Children {
		calculateTiming(child, clock)
		if child.Record.Duplicate || child.Record.End.Before(child.Record.Start) {
			continue
		}
		placed, usedConstraint := placeChild(node, child, clock)
		constrained = constrained || usedConstraint
		placements = append(placements, placed)
	}
	waitLower, waitUpper, waitNominal, certain, parallel := waitBounds(placements, duration)
	reason := "子调用位于同一设备，区间并集可确定"
	if !certain {
		reason = "子调用跨设备或存在时钟偏差；以可确定的包含/重叠关系计算等待时间上下界"
	}
	if constrained {
		reason += "；已应用研究者补充的时钟约束收紧区间"
	}
	if parallel {
		reason += "；并行区间已合并，未逐项相加"
	}
	node.Timing = model.TimingSplit{
		DurationMS:    duration,
		WaitMS:        model.DurationBreakdown{Nominal: waitNominal, Lower: waitLower, Upper: waitUpper, Certain: certain, Known: true, Reason: reason},
		ExecutionMS:   model.DurationBreakdown{Nominal: duration - waitNominal, Lower: duration - waitUpper, Upper: duration - waitLower, Certain: certain, Known: true},
		BoundsCertain: certain,
		Parallel:      parallel,
		Reason:        reason,
	}
}

// placeChild 用设备对偏移区间把子调用换算到父设备时钟下；没有约束依据时保持不确定。
func placeChild(node, child *model.CallNode, clock clockModel) (placement, bool) {
	rawStart := msBetween(node.Record.Start, child.Record.Start)
	rawEnd := msBetween(node.Record.Start, child.Record.End)
	placed := placement{rawStart: rawStart, rawEnd: rawEnd}
	if child.Record.DeviceID == node.Record.DeviceID {
		placed.certain = true
		placed.hasDef = true
		placed.defStart, placed.defEnd = rawStart, rawEnd
		placed.possStart, placed.possEnd = rawStart, rawEnd
		return placed, false
	}
	low, high, ok := clock.offset(node.Record.DeviceID, child.Record.DeviceID)
	if !ok {
		placed.unbounded = true
		placed.possStart, placed.possEnd = rawStart, rawEnd
		return placed, false
	}
	if low == high {
		placed.certain = true
		placed.hasDef = true
		placed.defStart, placed.defEnd = rawStart+low, rawEnd+low
		placed.possStart, placed.possEnd = rawStart+low, rawEnd+low
		return placed, true
	}
	placed.possStart, placed.possEnd = rawStart+low, rawEnd+high
	if defStart, defEnd := rawStart+high, rawEnd+low; defStart < defEnd {
		placed.hasDef = true
		placed.defStart, placed.defEnd = defStart, defEnd
	}
	return placed, true
}

// waitBounds 汇总各子调用的覆盖区间：并行重叠只按并集计一次，
// 缺失父调用或缺少约束时保留区间而不把各段最长可能耗时相加。
func waitBounds(placements []placement, parentDuration float64) (lower, upper, nominal float64, certain, parallel bool) {
	if len(placements) == 0 {
		return 0, 0, 0, true, false
	}
	certain = true
	definite := []interval{}
	possible := []interval{}
	raw := []interval{}
	hasUnbounded := false
	for _, placed := range placements {
		if !placed.certain {
			certain = false
		}
		if placed.unbounded {
			hasUnbounded = true
			if childDuration := placed.rawEnd - placed.rawStart; childDuration > lower {
				lower = childDuration
			}
		}
		if placed.hasDef {
			definite = append(definite, interval{start: placed.defStart, end: placed.defEnd})
		}
		possible = append(possible, interval{start: placed.possStart, end: placed.possEnd})
		raw = append(raw, interval{start: placed.rawStart, end: placed.rawEnd})
	}
	parallel = hasOverlap(possible)
	if certain {
		nominal = unionExact(definite)
		return nominal, nominal, nominal, true, parallel
	}
	if definiteUnion := unionExact(definite); definiteUnion > lower {
		lower = definiteUnion
	}
	lower = clamp(lower, 0, parentDuration)
	if hasUnbounded {
		upper = parentDuration
	} else {
		total := 0.0
		for _, placed := range placements {
			length := placed.rawEnd - placed.rawStart
			total += maxCoverage(placed.possStart, placed.possEnd-length, length, parentDuration)
		}
		upper = clamp(total, 0, parentDuration)
	}
	if upper < lower {
		upper = lower
	}
	nominal = clamp(unionExact(raw), lower, upper)
	return lower, upper, nominal, false, parallel
}

// maxCoverage 计算时长为 length、开始时刻落在 [sLow, sHigh] 的子调用
// 对父调用 [0, parent] 区间可能的最大覆盖时长。
func maxCoverage(sLow, sHigh, length, parent float64) float64 {
	if length >= parent && math.Max(sLow, parent-length) <= math.Min(sHigh, 0) {
		return parent
	}
	if math.Max(sLow, 0) <= math.Min(sHigh, parent-length) {
		return math.Min(length, parent)
	}
	best := 0.0
	for _, s := range []float64{sLow, sHigh} {
		if math.IsInf(s, 0) {
			continue
		}
		if coverage := math.Min(s+length, parent) - math.Max(s, 0); coverage > best {
			best = coverage
		}
	}
	return best
}

// assignAbsoluteBounds 自根向下推算每个节点开始/结束时刻相对根设备时钟的可能区间。
// 缺少约束或约束只给出单向边界时，保留 Known=false 而不是编造一个端点。
func assignAbsoluteBounds(root *model.CallNode, clock clockModel) {
	duration := msBetween(root.Record.Start, root.Record.End)
	root.Timing.StartMS = model.DurationBreakdown{Nominal: 0, Lower: 0, Upper: 0, Certain: true, Known: true}
	root.Timing.EndMS = model.DurationBreakdown{Nominal: duration, Lower: duration, Upper: duration, Certain: true, Known: true}
	assignChildBounds(root, clock)
}

func assignChildBounds(node *model.CallNode, clock clockModel) {
	for _, child := range node.Children {
		rawStart := msBetween(node.Record.Start, child.Record.Start)
		rawEnd := msBetween(node.Record.Start, child.Record.End)
		low, high, ok := clock.offset(node.Record.DeviceID, child.Record.DeviceID)
		finite := ok && !math.IsInf(low, 0) && !math.IsInf(high, 0)
		switch {
		case node.Timing.StartMS.Known && finite:
			certain := node.Timing.StartMS.Certain && low == high
			child.Timing.StartMS = model.DurationBreakdown{
				Nominal: rawStart, Lower: node.Timing.StartMS.Lower + rawStart + low, Upper: node.Timing.StartMS.Upper + rawStart + high,
				Certain: certain, Known: true,
			}
			child.Timing.EndMS = model.DurationBreakdown{
				Nominal: rawEnd, Lower: node.Timing.StartMS.Lower + rawEnd + low, Upper: node.Timing.StartMS.Upper + rawEnd + high,
				Certain: certain, Known: true,
			}
		case ok && !finite:
			reason := "时钟约束只给出单向边界，开始/结束时刻的另一端无法界定"
			child.Timing.StartMS = model.DurationBreakdown{Nominal: rawStart, Certain: false, Known: false, Reason: reason}
			child.Timing.EndMS = model.DurationBreakdown{Nominal: rawEnd, Certain: false, Known: false, Reason: reason}
		default:
			reason := "缺少该设备与父设备之间的时钟约束，开始/结束时刻区间无法界定"
			child.Timing.StartMS = model.DurationBreakdown{Nominal: rawStart, Certain: false, Known: false, Reason: reason}
			child.Timing.EndMS = model.DurationBreakdown{Nominal: rawEnd, Certain: false, Known: false, Reason: reason}
		}
		assignChildBounds(child, clock)
	}
}

func unionExact(values []interval) float64 {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]interval{}, values...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].start < sorted[j].start })
	total := 0.0
	start, end := sorted[0].start, sorted[0].end
	for _, item := range sorted[1:] {
		if item.start <= end && item.start >= start {
			if item.end > end {
				end = item.end
			}
		} else {
			total += end - start
			start, end = item.start, item.end
		}
	}
	return total + end - start
}

func hasOverlap(values []interval) bool {
	for i := range values {
		for j := i + 1; j < len(values); j++ {
			if values[i].start < values[j].end && values[j].start < values[i].end {
				return true
			}
		}
	}
	return false
}

func summarize(root *model.CallNode, count int) model.RequestSummary {
	anomalyCount := 0
	walk(root, func(n *model.CallNode) {
		if len(n.Record.Anomalies) > 0 {
			anomalyCount++
		}
	})
	return model.RequestSummary{RootID: root.Record.ID, TraceID: root.Record.TraceID, Service: root.Record.Service, Operation: root.Record.Operation, Start: root.Record.Start, DurationMS: root.Timing.DurationMS, WaitMS: root.Timing.WaitMS.Nominal, ExecutionMS: root.Timing.ExecutionMS.Nominal, SpanCount: count, AnomalyCount: anomalyCount, Certain: root.Timing.BoundsCertain}
}

func walk(node *model.CallNode, fn func(*model.CallNode)) {
	fn(node)
	for _, child := range node.Children {
		walk(child, fn)
	}
}

func msBetween(start, end time.Time) float64 { return float64(end.Sub(start).Microseconds()) / 1000.0 }
func clamp(v, low, high float64) float64 {
	if v < low {
		return low
	}
	if v > high {
		return high
	}
	return v
}
func appendUnique(values []string, value string) []string {
	for _, item := range values {
		if item == value {
			return values
		}
	}
	return append(values, value)
}

func cloneRecords(input []model.CallRecord) []model.CallRecord {
	out := make([]model.CallRecord, len(input))
	copy(out, input)
	return out
}
