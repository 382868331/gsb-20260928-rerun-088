package model

import "time"

type Batch struct {
	ID          string       `json:"id"`
	Source      string       `json:"source"`
	Format      string       `json:"format"`
	ImportedAt  time.Time    `json:"importedAt"`
	Description string       `json:"description,omitempty"`
	Records     []CallRecord `json:"records"`
	pending     map[string]int
}

type CallRecord struct {
	ID         string            `json:"id"`
	TraceID    string            `json:"traceId"`
	ParentID   string            `json:"parentId,omitempty"`
	Service    string            `json:"service"`
	Operation  string            `json:"operation"`
	DeviceID   string            `json:"deviceId"`
	Start      time.Time         `json:"start"`
	End        time.Time         `json:"end"`
	DurationMS DurationBreakdown `json:"durationMs"`
	Attributes map[string]any    `json:"attributes,omitempty"`
	Raw        map[string]any    `json:"raw,omitempty"`
	Evidence   []Evidence        `json:"evidence,omitempty"`
	Anomalies  []string          `json:"anomalies,omitempty"`
	Duplicate  bool              `json:"duplicate,omitempty"`
}

type DurationBreakdown struct {
	Nominal float64 `json:"nominal"`
	Lower   float64 `json:"lower,omitempty"`
	Upper   float64 `json:"upper,omitempty"`
	Certain bool    `json:"certain"`
	Reason  string  `json:"reason,omitempty"`
}

// ClockConstraint 是研究者在原始时间戳之外补充的时钟依据。
// Type=before：after 事件比 before 事件晚 MinMS..MaxMS 毫秒（发送早于接收）。
// Type=offset：target 事件相对 anchor 事件的时间差落在 MinMS..MaxMS。
// 依据只引用原始记录的开始/结束时刻，不修改原始时间戳。
type ClockConstraint struct {
	ID        string  `json:"id"`
	Type      string  `json:"type"`
	Before    string  `json:"before,omitempty"`
	After     string  `json:"after,omitempty"`
	Anchor    string  `json:"anchor,omitempty"`
	Target    string  `json:"target,omitempty"`
	MinMS     float64 `json:"minMs"`
	MaxMS     float64 `json:"maxMs"`
	Rationale string  `json:"rationale,omitempty"`
	// Disabled 为 true 时约束暂时停用、不参与求解，但保留在假设中可随时恢复。
	Disabled bool `json:"disabled,omitempty"`
}

const (
	ConstraintBefore = "before"
	ConstraintOffset = "offset"
)

// ClockConflict 描述相互矛盾、无法同时成立的时钟依据，
// 并指出涉及的原始记录与约束，系统不挑选某个精确时刻掩盖冲突。
type ClockConflict struct {
	Type          string   `json:"type"`
	ConstraintIDs []string `json:"constraintIds"`
	RecordIDs     []string `json:"recordIds"`
	Summary       string   `json:"summary"`
}

// ClockReport 汇总一次时钟假设下的求解结果。
type ClockReport struct {
	Feasible           bool            `json:"feasible"`
	Conflicts          []ClockConflict `json:"conflicts,omitempty"`
	ConstraintIDs      []string        `json:"constraintIds,omitempty"`
	DisabledIDs        []string        `json:"disabledConstraintIds,omitempty"`
	UncertainRecordIDs []string        `json:"uncertainRecordIds,omitempty"`
}

type Evidence struct {
	Type    string         `json:"type"`
	Summary string         `json:"summary"`
	Detail  map[string]any `json:"detail,omitempty"`
}

type Investigation struct {
	ID         string          `json:"id"`
	BatchID    string          `json:"batchId"`
	Name       string          `json:"name"`
	RootID     string          `json:"rootId"`
	TraceID    string          `json:"traceId"`
	Selected   string          `json:"selectedId,omitempty"`
	Notes      string          `json:"notes,omitempty"`
	CreatedAt  time.Time       `json:"createdAt"`
	UpdatedAt  time.Time       `json:"updatedAt"`
	View       ViewState       `json:"view"`
	Hypothesis ClockHypothesis `json:"hypothesis,omitempty"`
}

type ViewState struct {
	ShowAnomaliesOnly bool     `json:"showAnomaliesOnly,omitempty"`
	ExpandedIDs       []string `json:"expandedIds,omitempty"`
	TimeMode          string   `json:"timeMode,omitempty"`
}

// ClockHypothesis 是一次调查采用的时钟假设：约束集合与尚未解决的部分。
type ClockHypothesis struct {
	Name        string            `json:"name,omitempty"`
	Constraints []ClockConstraint `json:"constraints,omitempty"`
	Unresolved  []string          `json:"unresolved,omitempty"`
}

type RequestSummary struct {
	RootID       string    `json:"rootId"`
	TraceID      string    `json:"traceId"`
	Service      string    `json:"service"`
	Operation    string    `json:"operation"`
	Start        time.Time `json:"start"`
	DurationMS   float64   `json:"durationMs"`
	WaitMS       float64   `json:"waitMs"`
	ExecutionMS  float64   `json:"executionMs"`
	SpanCount    int       `json:"spanCount"`
	AnomalyCount int       `json:"anomalyCount"`
	Certain      bool      `json:"certain"`
}

type Analysis struct {
	Batch       Batch                `json:"batch"`
	Requests    []RequestSummary     `json:"requests"`
	Traces      map[string]*CallNode `json:"traces"`
	Anomalies   []Anomaly            `json:"anomalies"`
	Clock       ClockReport          `json:"clock"`
	GeneratedAt time.Time            `json:"generatedAt"`
}

type CallNode struct {
	Record   CallRecord  `json:"record"`
	Children []*CallNode `json:"children"`
	Timing   TimingSplit `json:"timing"`
	Level    int         `json:"level"`
}

type TimingSplit struct {
	DurationMS    float64           `json:"durationMs"`
	WaitMS        DurationBreakdown `json:"waitMs"`
	ExecutionMS   DurationBreakdown `json:"executionMs"`
	StartOffsetMS DurationBreakdown `json:"startOffsetMs,omitempty"`
	EndOffsetMS   DurationBreakdown `json:"endOffsetMs,omitempty"`
	BoundsCertain bool              `json:"boundsCertain"`
	// AssumptionBased 为 true 表示区间上界/下界由研究者的时钟假设收紧，
	// 而非原始时间戳可直接证明；原始记录本身从未被修改。
	AssumptionBased bool   `json:"assumptionBased,omitempty"`
	Parallel        bool   `json:"parallel"`
	Reason          string `json:"reason,omitempty"`
}

type Anomaly struct {
	Type     string `json:"type"`
	RecordID string `json:"recordId,omitempty"`
	TraceID  string `json:"traceId,omitempty"`
	Summary  string `json:"summary"`
}

type ComparisonSide struct {
	InvestigationID string            `json:"investigationId"`
	Name            string            `json:"name"`
	RootID          string            `json:"rootId"`
	TraceID         string            `json:"traceId"`
	DurationMS      float64           `json:"durationMs"`
	ExecutionMS     DurationBreakdown `json:"executionMs"`
	WaitMS          DurationBreakdown `json:"waitMs"`
	Certain         bool              `json:"certain"`
	Parallel        bool              `json:"parallel"`
	AnomalyCount    int               `json:"anomalyCount"`
}

type Comparison struct {
	A                  ComparisonSide    `json:"a"`
	B                  ComparisonSide    `json:"b"`
	DurationDeltaMS    float64           `json:"durationDeltaMs"`
	ExecutionDelta     DurationBreakdown `json:"executionDelta"`
	WaitDelta          DurationBreakdown `json:"waitDelta"`
	Attribution        string            `json:"attribution"`
	EvidenceIncomplete bool              `json:"evidenceIncomplete"`
	// AssumptionChange 为 true 表示两次调查基于同一批次但时钟假设不同，
	// 此时区间差异可能仅来自假设收紧，而非真实耗时变化。
	AssumptionChange bool     `json:"assumptionChange"`
	Notes            []string `json:"notes"`
}

// ReviewBundle 是可导出的调查复核包：原始记录、启用与停用的时钟依据、
// 冲突链与调查对照结论一并打包，另一台同版本应用导入后可按原依据重算，
// 并明确区分原保存结论与重新求解结果。
type ReviewBundle struct {
	// Format 固定为 review-bundle-v1，后续格式演进新增版本，不改动已导出包。
	Format        string        `json:"format"`
	ExportedAt    time.Time     `json:"exportedAt"`
	Investigation Investigation `json:"investigation"`
	Batch         Batch         `json:"batch"`
	// RelatedInvestigations 为对照结论涉及的另一次调查（通常同批次、不同假设）。
	RelatedInvestigations []Investigation `json:"relatedInvestigations,omitempty"`
	// SavedConclusions 是导出时保存的求解结论（原保存结论）。
	SavedConclusions SavedConclusions `json:"savedConclusions"`
	// Comparison 是导出时计算的调查对照结论。
	Comparison *Comparison `json:"comparison,omitempty"`
}

const ReviewBundleFormat = "review-bundle-v1"

// SavedConclusions 是导出时刻的求解结论快照，导入方重算后与此对照，
// 两者并列展示，不用重算结果覆盖原保存结论。
type SavedConclusions struct {
	Clock    ClockReport      `json:"clock"`
	Requests []RequestSummary `json:"requests"`
}

// ReviewProblem 描述导入复核包时发现但不静默丢弃的问题：
// 缺失记录、重复 ID 等，依据保留，问题列入清单。
type ReviewProblem struct {
	Type    string `json:"type"`
	ID      string `json:"id,omitempty"`
	Summary string `json:"summary"`
}

const (
	ProblemMissingRecord           = "missing_record"
	ProblemDuplicateRecordID       = "duplicate_record_id"
	ProblemDuplicateConstraintID   = "duplicate_constraint_id"
	ProblemBatchMismatch           = "batch_mismatch"
	ProblemComparisonNotRecomputed = "comparison_not_recomputed"
)

// ReviewImportResult 是导入复核包的结果：原保存结论与重新求解结果并列，
// 问题清单完整保留。
type ReviewImportResult struct {
	Investigation        Investigation    `json:"investigation"`
	BatchID              string           `json:"batchId"`
	Problems             []ReviewProblem  `json:"problems"`
	SavedConclusions     SavedConclusions `json:"savedConclusions"`
	Recomputed           Analysis         `json:"recomputed"`
	SavedComparison      *Comparison      `json:"savedComparison,omitempty"`
	RecomputedComparison *Comparison      `json:"recomputedComparison,omitempty"`
}
