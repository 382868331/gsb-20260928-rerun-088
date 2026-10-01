package analysis

import (
	"fmt"
	"sort"
	"time"

	"performance-investigation/internal/model"
)

type interval struct {
	start, end float64
	certain    bool
	device     string
}

func Analyze(batch model.Batch) model.Analysis {
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
	for _, list := range traceRecords {
		sort.SliceStable(list, func(i, j int) bool {
			if list[i].Start.Equal(list[j].Start) {
				return list[i].ID < list[j].ID
			}
			return list[i].Start.Before(list[j].Start)
		})
	}

	traces := map[string]*model.CallNode{}
	summaries := []model.RequestSummary{}
	traceIDs := make([]string, 0, len(traceRecords))
	for traceID := range traceRecords {
		traceIDs = append(traceIDs, traceID)
	}
	sort.Strings(traceIDs)
	for _, traceID := range traceIDs {
		list := traceRecords[traceID]
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
		sort.SliceStable(roots, func(i, j int) bool {
			if roots[i].Record.Start.Equal(roots[j].Record.Start) {
				return roots[i].Record.ID < roots[j].Record.ID
			}
			return roots[i].Record.Start.Before(roots[j].Record.Start)
		})
		var root *model.CallNode
		if len(roots) == 1 {
			root = roots[0]
		} else if len(roots) > 1 {
			root = virtualRoot(traceID, roots)
			children[root.Record.ID] = roots
			for _, extra := range roots {
				anomalies = append(anomalies, model.Anomaly{Type: "multiple_roots", RecordID: extra.Record.ID, TraceID: traceID, Summary: fmt.Sprintf("调用 %s 缺少可验证父级，作为悬挂根节点保留在虚拟根下", extra.Record.ID)})
			}
		} else {
			continue
		}
		assignChildren(root, children, 0)
		calculateTiming(root)
		traces[traceID] = root
		summaries = append(summaries, summarize(root, len(list)))
	}

	sort.SliceStable(summaries, func(i, j int) bool {
		if summaries[i].Start.Equal(summaries[j].Start) {
			if summaries[i].TraceID == summaries[j].TraceID {
				return summaries[i].RootID < summaries[j].RootID
			}
			return summaries[i].TraceID < summaries[j].TraceID
		}
		return summaries[i].Start.After(summaries[j].Start)
	})
	sort.Slice(anomalies, func(i, j int) bool {
		if anomalies[i].TraceID == anomalies[j].TraceID {
			return anomalies[i].RecordID < anomalies[j].RecordID
		}
		return anomalies[i].TraceID < anomalies[j].TraceID
	})
	batch.Records = records
	return model.Analysis{Batch: batch, Requests: summaries, Traces: traces, Anomalies: anomalies, GeneratedAt: time.Now().UTC()}
}

func virtualRoot(traceID string, roots []*model.CallNode) *model.CallNode {
	start := roots[0].Record.Start
	end := roots[0].Record.End
	device := roots[0].Record.DeviceID
	for _, node := range roots[1:] {
		if node.Record.Start.Before(start) {
			start = node.Record.Start
		}
		if node.Record.End.After(end) {
			end = node.Record.End
		}
		if node.Record.DeviceID != device {
			device = ""
		}
	}
	return &model.CallNode{Record: model.CallRecord{
		ID:        traceID + "__roots",
		TraceID:   traceID,
		Service:   "multiple-roots",
		Operation: fmt.Sprintf("%d 个缺少可验证父级的根调用", len(roots)),
		DeviceID:  device,
		Start:     start,
		End:       end,
		Anomalies: []string{"multiple_roots"},
	}}
}

func assignChildren(node *model.CallNode, children map[string][]*model.CallNode, level int) {
	node.Level = level
	kids := children[node.Record.ID]
	if kids == nil {
		kids = []*model.CallNode{}
	}
	sort.SliceStable(kids, func(i, j int) bool {
		if kids[i].Record.Start.Equal(kids[j].Record.Start) {
			return kids[i].Record.ID < kids[j].Record.ID
		}
		return kids[i].Record.Start.Before(kids[j].Record.Start)
	})
	node.Children = kids
	for _, child := range kids {
		assignChildren(child, children, level+1)
	}
}

func calculateTiming(node *model.CallNode) {
	duration := msBetween(node.Record.Start, node.Record.End)
	if duration < 0 {
		duration = 0
	}
	intervals := []interval{}
	for _, child := range node.Children {
		calculateTiming(child)
		if child.Record.Duplicate || child.Record.End.Before(child.Record.Start) {
			continue
		}
		start := msBetween(node.Record.Start, child.Record.Start)
		end := msBetween(node.Record.Start, child.Record.End)
		intervals = append(intervals, interval{start: start, end: end, certain: child.Record.DeviceID == node.Record.DeviceID, device: child.Record.DeviceID})
	}
	waitLower, waitUpper, waitNominal, certain, parallel := unionBounds(intervals, duration)
	if waitLower < 0 {
		waitLower = 0
	}
	if waitUpper > duration {
		waitUpper = duration
	}
	if waitNominal < 0 || waitNominal > duration {
		waitNominal = clamp(waitNominal, 0, duration)
	}
	reason := "子调用位于同一设备，区间并集可确定"
	if !certain {
		reason = "子调用跨设备或存在时钟偏差；以可确定的包含/重叠关系计算等待时间上下界"
	}
	if parallel {
		reason += "；并行区间已合并，未逐项相加"
	}
	node.Timing = model.TimingSplit{
		DurationMS:    duration,
		WaitMS:        model.DurationBreakdown{Nominal: waitNominal, Lower: waitLower, Upper: waitUpper, Certain: certain, Reason: reason},
		ExecutionMS:   model.DurationBreakdown{Nominal: duration - waitNominal, Lower: duration - waitUpper, Upper: duration - waitLower, Certain: certain},
		BoundsCertain: certain,
		Parallel:      parallel,
		Reason:        reason,
	}
}

func unionBounds(values []interval, parentDuration float64) (lower, upper, nominal float64, certain, parallel bool) {
	if len(values) == 0 {
		return 0, 0, 0, true, false
	}
	certain = true
	for _, value := range values {
		if !value.certain {
			certain = false
		}
	}
	if certain {
		nominal = unionExact(values)
		return nominal, nominal, nominal, true, hasOverlap(values)
	}
	certainValues := make([]interval, 0, len(values))
	for _, value := range values {
		if value.certain {
			certainValues = append(certainValues, value)
		} else if childDuration := value.end - value.start; childDuration > lower {
			lower = childDuration
		}
	}
	if certainUnion := unionExact(certainValues); certainUnion > lower {
		lower = certainUnion
	}
	upper = parentDuration
	nominal = clamp(unionExact(values), 0, parentDuration)
	parallel = hasOverlap(certainValues)
	return lower, upper, nominal, false, parallel
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
