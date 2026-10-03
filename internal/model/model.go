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
	UncertainRecordIDs []string        `json:"uncertainRecordIds,omitempty"`
}

type Evidence struct {
	Type    string         `json:"type"`
	Summary string         `json:"summary"`
	Detail  map[string]any `json:"detail,omitempty"`
}

type Investigation struct {
	ID        string    `json:"id"`
	BatchID   string    `json:"batchId"`
	Name      string    `json:"name"`
	RootID    string    `json:"rootId"`
	TraceID   string    `json:"traceId"`
	Selected  string    `json:"selectedId,omitempty"`
	Notes     string    `json:"notes,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
	View      ViewState `json:"view"`
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
	Parallel      bool              `json:"parallel"`
	Reason        string            `json:"reason,omitempty"`
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
	Notes              []string          `json:"notes"`
}
