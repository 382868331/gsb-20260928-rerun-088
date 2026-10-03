package analysis

import (
	"fmt"
	"math"
	"sort"

	"performance-investigation/internal/model"
)

// devicePair 以字典序为规范方向，bounds 表示 o[b] - o[a]（设备 b 时钟减去设备 a 时钟）的可信区间。
type devicePair struct{ a, b string }

type offsetBound struct {
	low, high     float64
	constraintIDs []string
	recordIDs     map[string]bool
}

type clockModel struct {
	bounds    map[devicePair]*offsetBound
	anomalies []model.Anomaly
}

// buildClockModel 把研究者补充的时钟依据换算成设备对之间的时钟偏移区间。
// 约束相互矛盾时不自动选一个精确时刻：保留先到的区间，并把冲突涉及的
// 约束与原记录作为 constraint_conflict 异常抛出。
func buildClockModel(records []model.CallRecord, constraints []model.ClockConstraint) clockModel {
	byID := map[string]model.CallRecord{}
	for _, rec := range records {
		if rec.ID != "" {
			byID[rec.ID] = rec
		}
	}
	result := clockModel{bounds: map[devicePair]*offsetBound{}}
	for _, constraint := range constraints {
		recA, okA := byID[constraint.ARecordID]
		recB, okB := byID[constraint.BRecordID]
		if !okA || !okB {
			missing := constraint.ARecordID
			if okA {
				missing = constraint.BRecordID
			}
			result.anomalies = append(result.anomalies, model.Anomaly{
				Type:          "constraint_unknown_record",
				RecordIDs:     []string{constraint.ARecordID, constraint.BRecordID},
				ConstraintIDs: []string{constraint.ID},
				Summary:       fmt.Sprintf("时钟约束 %s 引用的记录 %s 不在本批次中，该约束未参与推算", constraint.ID, missing),
			})
			continue
		}
		low, high, ok := constraintBound(constraint, recA, recB)
		if !ok {
			result.anomalies = append(result.anomalies, model.Anomaly{
				Type:          "constraint_unknown_record",
				RecordIDs:     []string{recA.ID, recB.ID},
				ConstraintIDs: []string{constraint.ID},
				Summary:       fmt.Sprintf("时钟约束 %s 类型 %q 无法识别，未参与推算", constraint.ID, constraint.Type),
			})
			continue
		}
		pair := devicePair{recA.DeviceID, recB.DeviceID}
		if pair.a == pair.b {
			// 同设备约束：直接校验原始时间戳是否已满足，不满足即矛盾。
			if violatesSameDevice(constraint, recA, recB) {
				result.anomalies = append(result.anomalies, conflictAnomaly(constraint.ID, nil, []string{recA.ID, recB.ID},
					fmt.Sprintf("时钟约束 %s 与同设备原始时间戳矛盾：记录 %s 与 %s 的原始先后顺序已违反该约束", constraint.ID, recA.ID, recB.ID)))
			}
			continue
		}
		if pair.a > pair.b {
			pair = devicePair{pair.b, pair.a}
			low, high = -high, -low
		}
		recordIDs := []string{recA.ID, recB.ID}
		existing := result.bounds[pair]
		if existing == nil {
			result.bounds[pair] = &offsetBound{low: low, high: high, constraintIDs: []string{constraint.ID}, recordIDs: idSet(recordIDs)}
			continue
		}
		newLow, newHigh := math.Max(existing.low, low), math.Min(existing.high, high)
		if newLow > newHigh {
			involved := unionIDs(existing.recordIDs, recordIDs)
			constraintIDs := append(append([]string{}, existing.constraintIDs...), constraint.ID)
			result.anomalies = append(result.anomalies, conflictAnomaly(constraint.ID, constraintIDs, involved,
				fmt.Sprintf("时钟约束相互矛盾：约束 %v 对设备 %s/%s 给出的偏移区间没有交集，涉及原记录 %v；未自动选取任何精确时刻", constraintIDs, pair.a, pair.b, involved)))
			continue
		}
		existing.low, existing.high = newLow, newHigh
		existing.constraintIDs = append(existing.constraintIDs, constraint.ID)
		for _, id := range recordIDs {
			existing.recordIDs[id] = true
		}
	}
	return result
}

// constraintBound 把一条约束换算成 o[B.device] - o[A.device] 的区间（毫秒）。
func constraintBound(constraint model.ClockConstraint, recA, recB model.CallRecord) (float64, float64, bool) {
	switch constraint.Type {
	case "order":
		// A 的结束必然早于 B 的开始：(B.start + oB) - (A.end + oA) >= 0
		return msBetween(recB.Start, recA.End), math.Inf(1), true
	case "offset_range":
		delta := msBetween(recA.Start, recB.Start)
		low, high := math.Inf(-1), math.Inf(1)
		if constraint.LowerMS != nil {
			low = *constraint.LowerMS - delta
		}
		if constraint.UpperMS != nil {
			high = *constraint.UpperMS - delta
		}
		if low > high {
			return 0, 0, false
		}
		return low, high, true
	default:
		return 0, 0, false
	}
}

func violatesSameDevice(constraint model.ClockConstraint, recA, recB model.CallRecord) bool {
	if constraint.Type != "order" {
		if constraint.Type == "offset_range" {
			delta := msBetween(recA.Start, recB.Start)
			if constraint.LowerMS != nil && delta < *constraint.LowerMS {
				return true
			}
			if constraint.UpperMS != nil && delta > *constraint.UpperMS {
				return true
			}
		}
		return false
	}
	return recB.Start.Before(recA.End)
}

// offset 返回 o[device] - o[base] 的可信区间；没有约束依据时 ok=false。
func (m clockModel) offset(base, device string) (float64, float64, bool) {
	if base == device {
		return 0, 0, true
	}
	if bound := m.bounds[devicePair{base, device}]; bound != nil {
		return bound.low, bound.high, true
	}
	if bound := m.bounds[devicePair{device, base}]; bound != nil {
		return -bound.high, -bound.low, true
	}
	return 0, 0, false
}

func conflictAnomaly(_ string, constraintIDs []string, recordIDs []string, summary string) model.Anomaly {
	sort.Strings(recordIDs)
	sort.Strings(constraintIDs)
	return model.Anomaly{Type: "constraint_conflict", RecordIDs: recordIDs, ConstraintIDs: constraintIDs, Summary: summary}
}

func idSet(ids []string) map[string]bool {
	set := map[string]bool{}
	for _, id := range ids {
		set[id] = true
	}
	return set
}

func unionIDs(set map[string]bool, ids []string) []string {
	out := map[string]bool{}
	for id := range set {
		out[id] = true
	}
	for _, id := range ids {
		out[id] = true
	}
	list := make([]string, 0, len(out))
	for id := range out {
		list = append(list, id)
	}
	sort.Strings(list)
	return list
}
