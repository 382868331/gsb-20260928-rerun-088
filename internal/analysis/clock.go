package analysis

import (
	"fmt"
	"math"
	"sort"
	"time"

	"performance-investigation/internal/model"
)

// eventRef 标识某条原始记录的开始或结束时刻。
func eventRef(recordID, end string) string { return recordID + ":" + end }

type edge struct {
	from, to int
	weight   float64
	source   string // 约束 ID，或 "device:<id>"
	records  []string
}

type clockSolver struct {
	refs     []string
	index    map[string]int
	edges    []edge
	dist     [][]float64
	feasible bool
}

// solveClock 在不修改原始时间戳的前提下，把同设备时钟读数与研究者补充的
// 时钟依据统一为差分约束系统，求每个事件相对其他事件的可达时间区间。
// 约束相互矛盾时返回矛盾涉及的原始记录与约束，不挑选精确时刻掩盖冲突。
// disabled 中的记录被暂时停用：不参与求解，也不作为约束端点。
func solveClock(records []model.CallRecord, constraints []model.ClockConstraint, disabled map[string]bool) (*clockSolver, []model.ClockConflict) {
	solver := &clockSolver{index: map[string]int{}, feasible: true}
	conflicts := []model.ClockConflict{}
	valid := map[string]bool{}
	for _, rec := range records {
		if rec.Duplicate || disabled[rec.ID] {
			continue
		}
		valid[rec.ID] = true
		solver.addEvent(eventRef(rec.ID, "start"))
		solver.addEvent(eventRef(rec.ID, "end"))
	}

	// 同一设备上的时钟读数次序与差值按名义时间戳精确固定；时间倒置记录不加入。
	byDevice := map[string][]model.CallRecord{}
	for _, rec := range records {
		if rec.Duplicate || disabled[rec.ID] || rec.End.Before(rec.Start) {
			continue
		}
		byDevice[rec.DeviceID] = append(byDevice[rec.DeviceID], rec)
	}
	deviceIDs := make([]string, 0, len(byDevice))
	for deviceID := range byDevice {
		deviceIDs = append(deviceIDs, deviceID)
	}
	sort.Strings(deviceIDs)
	for _, deviceID := range deviceIDs {
		type ev struct {
			ref string
			t   time.Time
		}
		evList := []ev{}
		recOfRef := map[string]string{}
		for _, rec := range byDevice[deviceID] {
			evList = append(evList, ev{eventRef(rec.ID, "start"), rec.Start}, ev{eventRef(rec.ID, "end"), rec.End})
			recOfRef[eventRef(rec.ID, "start")] = rec.ID
			recOfRef[eventRef(rec.ID, "end")] = rec.ID
		}
		sort.Slice(evList, func(i, j int) bool { return evList[i].t.Before(evList[j].t) })
		for i := 1; i < len(evList); i++ {
			delta := ms(evList[i].t.Sub(evList[i-1].t))
			src := "device:" + deviceID
			recs := []string{recOfRef[evList[i-1].ref], recOfRef[evList[i].ref]}
			solver.addEdge(evList[i-1].ref, evList[i].ref, delta, src, recs)
			solver.addEdge(evList[i].ref, evList[i-1].ref, -delta, src, recs)
		}
	}

	usedConstraints := []string{}
	for _, constraint := range constraints {
		from, to, ok := constraintEndpoints(constraint)
		if !ok {
			conflicts = append(conflicts, model.ClockConflict{Type: "invalid_constraint", ConstraintIDs: []string{constraint.ID}, Summary: fmt.Sprintf("约束 %s 类型或端点缺失，未参与求解", constraint.ID)})
			continue
		}
		if !valid[recordOfRef(from)] || !valid[recordOfRef(to)] {
			kind := "unknown_record"
			summary := fmt.Sprintf("约束 %s 引用了批次中不存在或重复的原始记录", constraint.ID)
			if disabled[recordOfRef(from)] || disabled[recordOfRef(to)] {
				kind = "excluded_record"
				summary = fmt.Sprintf("约束 %s 引用了已暂时停用的记录，未参与求解", constraint.ID)
			}
			conflicts = append(conflicts, model.ClockConflict{Type: kind, ConstraintIDs: []string{constraint.ID}, RecordIDs: []string{recordOfRef(from), recordOfRef(to)}, Summary: summary})
			continue
		}
		if constraint.MaxMS < constraint.MinMS {
			conflicts = append(conflicts, model.ClockConflict{Type: "invalid_range", ConstraintIDs: []string{constraint.ID}, RecordIDs: []string{recordOfRef(from), recordOfRef(to)}, Summary: fmt.Sprintf("约束 %s 的时差范围 %v..%v 不成立（上界小于下界）", constraint.ID, constraint.MinMS, constraint.MaxMS)})
			continue
		}
		recs := uniqStrings([]string{recordOfRef(from), recordOfRef(to)})
		solver.addEdge(from, to, constraint.MaxMS, constraint.ID, recs)
		solver.addEdge(to, from, -constraint.MinMS, constraint.ID, recs)
		usedConstraints = append(usedConstraints, constraint.ID)
	}

	if cycle := solver.negativeCycle(); cycle != nil {
		conflictIDs := []string{}
		recordIDs := []string{}
		for _, edgeIndex := range cycle {
			e := solver.edges[edgeIndex]
			if e.source != "" && !isDeviceSource(e.source) {
				conflictIDs = append(conflictIDs, e.source)
			}
			recordIDs = append(recordIDs, e.records...)
		}
		conflicts = append(conflicts, model.ClockConflict{Type: "contradiction", ConstraintIDs: uniqStrings(conflictIDs), RecordIDs: uniqStrings(recordIDs), Summary: "时钟约束相互矛盾：不存在能同时满足全部依据的时刻，已列出涉及的原始记录与约束"})
		solver.feasible = false
		// 退回仅依据同设备时钟读数的求解，保留区间，不采用任何矛盾假设。
		fallback := &clockSolver{index: map[string]int{}, feasible: true}
		for _, ref := range solver.refs {
			fallback.addEvent(ref)
		}
		for _, e := range solver.edges {
			if isDeviceSource(e.source) {
				fallback.edges = append(fallback.edges, e)
			}
		}
		fallback.floyd()
		fallback.feasible = false
		return fallback, conflicts
	}

	solver.floyd()
	_ = usedConstraints
	return solver, conflicts
}

func constraintEndpoints(constraint model.ClockConstraint) (from, to string, ok bool) {
	switch constraint.Type {
	case model.ConstraintBefore:
		if constraint.Before == "" || constraint.After == "" {
			return "", "", false
		}
		return constraint.Before, constraint.After, true
	case model.ConstraintOffset:
		if constraint.Anchor == "" || constraint.Target == "" {
			return "", "", false
		}
		return constraint.Anchor, constraint.Target, true
	default:
		return "", "", false
	}
}

func recordOfRef(ref string) string {
	for i := len(ref) - 1; i >= 0; i-- {
		if ref[i] == ':' {
			return ref[:i]
		}
	}
	return ref
}

func (s *clockSolver) addEvent(ref string) int {
	if id, ok := s.index[ref]; ok {
		return id
	}
	id := len(s.refs)
	s.refs = append(s.refs, ref)
	s.index[ref] = id
	return id
}

func (s *clockSolver) addEdge(from, to string, weight float64, source string, records []string) {
	s.edges = append(s.edges, edge{from: s.addEvent(from), to: s.addEvent(to), weight: weight, source: source, records: records})
}

// negativeCycle 用 Bellman-Ford 检测负环并返回环上的边索引；
// 差分约束增广了源点 0 权边，因此任意不可达分量也能被检出。
func (s *clockSolver) negativeCycle() []int {
	n := len(s.refs)
	dist := make([]float64, n)
	prev := make([]int, n)
	for i := range prev {
		prev[i] = -1
	}
	relaxed := false
	for iter := 0; iter < n; iter++ {
		relaxed = false
		for edgeIndex, e := range s.edges {
			if dist[e.from]+e.weight < dist[e.to]-1e-9 {
				dist[e.to] = dist[e.from] + e.weight
				prev[e.to] = edgeIndex
				relaxed = true
			}
		}
		if !relaxed {
			break
		}
	}
	if !relaxed {
		return nil
	}
	// 再松弛一次找到仍在下降的节点，沿前驱回溯提取负环。
	var vertex int = -1
	var edgeIndex int = -1
	for ei, e := range s.edges {
		if dist[e.from]+e.weight < dist[e.to]-1e-9 {
			vertex, edgeIndex = e.to, ei
			break
		}
	}
	if vertex < 0 {
		return nil
	}
	seen := map[int]int{}
	cycle := []int{edgeIndex}
	cur := vertex
	for {
		if startEdge, ok := seen[cur]; ok {
			return cycle[startEdge:]
		}
		seen[cur] = len(cycle)
		ei := prev[cur]
		if ei < 0 {
			break
		}
		cycle = append(cycle, ei)
		cur = s.edges[ei].from
	}
	return cycle
}

func (s *clockSolver) floyd() {
	n := len(s.refs)
	s.dist = make([][]float64, n)
	for i := range s.dist {
		s.dist[i] = make([]float64, n)
		for j := range s.dist[i] {
			s.dist[i][j] = math.Inf(1)
		}
		s.dist[i][i] = 0
	}
	for _, e := range s.edges {
		if e.weight < s.dist[e.from][e.to] {
			s.dist[e.from][e.to] = e.weight
		}
	}
	for k := 0; k < n; k++ {
		for i := 0; i < n; i++ {
			if math.IsInf(s.dist[i][k], 1) {
				continue
			}
			for j := 0; j < n; j++ {
				if candidate := s.dist[i][k] + s.dist[k][j]; candidate < s.dist[i][j] {
					s.dist[i][j] = candidate
				}
			}
		}
	}
}

// offsetBounds 返回 x(to)-x(from) 的可证明区间 [lower, upper]。
func (s *clockSolver) offsetBounds(from, to string) (lower, upper float64, ok bool) {
	i, iok := s.index[from]
	j, jok := s.index[to]
	if !iok || !jok {
		return 0, 0, false
	}
	upper = s.dist[i][j]
	lower = -s.dist[j][i]
	if math.IsInf(upper, 1) || math.IsInf(lower, -1) {
		return 0, 0, false
	}
	return lower, upper, true
}

// ordered 报告 to 是否必然晚于 from（区间下界大于 0）。
func (s *clockSolver) ordered(from, to string) (decided, after bool) {
	lower, _, ok := s.offsetBounds(from, to)
	if !ok {
		return false, false
	}
	if lower > 1e-9 {
		return true, true
	}
	upper := -lower
	if _, up, ok2 := s.offsetBounds(from, to); ok2 {
		upper = up
	}
	if upper < -1e-9 {
		return true, false
	}
	return false, false
}

func isDeviceSource(source string) bool { return len(source) >= 7 && source[:7] == "device:" }

func ms(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }

func uniqStrings(values []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, value := range values {
		if value != "" && !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	sort.Strings(out)
	return out
}
