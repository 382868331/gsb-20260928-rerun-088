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
	// Known 为 false 表示连区间都无法界定（缺少时钟约束），此时 Lower/Upper 不具意义。
	Known  bool   `json:"known,omitempty"`
	Reason string `json:"reason,omitempty"`
}

// ClockConstraint 是研究者补充的时钟依据：两条记录之间的先后顺序，
// 或两条记录开始时刻之差的可信范围。约束只参与分析推算，不回写原始时间戳。
type ClockConstraint struct {
	ID        string   `json:"id"`
	Type      string   `json:"type"` // "order"：A 的结束必然早于 B 的开始；"offset_range"：B.start - A.start 落在 [LowerMS, UpperMS]
	ARecordID string   `json:"aRecordId"`
	BRecordID string   `json:"bRecordId"`
	LowerMS   *float64 `json:"lowerMs,omitempty"`
	UpperMS   *float64 `json:"upperMs,omitempty"`
	Note      string   `json:"note,omitempty"`
}

// SetKnown 标记区间为可界定，便于链式构造。
func (d DurationBreakdown) SetKnown() DurationBreakdown {
	d.Known = true
	return d
}

type Evidence struct {
	Type    string         `json:"type"`
	Summary string         `json:"summary"`
	Detail  map[string]any `json:"detail,omitempty"`
}

type Investigation struct {
	ID          string            `json:"id"`
	BatchID     string            `json:"batchId"`
	Name        string            `json:"name"`
	RootID      string            `json:"rootId"`
	TraceID     string            `json:"traceId"`
	Selected    string            `json:"selectedId,omitempty"`
	Notes       string            `json:"notes,omitempty"`
	Assumptions []ClockConstraint `json:"assumptions,omitempty"`
	CreatedAt   time.Time         `json:"createdAt"`
	UpdatedAt   time.Time         `json:"updatedAt"`
	View        ViewState         `json:"view"`
}

type ViewState struct {
	ShowAnomaliesOnly bool     `json:"showAnomaliesOnly,omitempty"`
	ExpandedIDs       []string `json:"expandedIds,omitempty"`
	TimeMode          string   `json:"timeMode,omitempty"`
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
	StartMS       DurationBreakdown `json:"startMs"`
	EndMS         DurationBreakdown `json:"endMs"`
	WaitMS        DurationBreakdown `json:"waitMs"`
	ExecutionMS   DurationBreakdown `json:"executionMs"`
	BoundsCertain bool              `json:"boundsCertain"`
	Parallel      bool              `json:"parallel"`
	Reason        string            `json:"reason,omitempty"`
}

type Anomaly struct {
	Type          string   `json:"type"`
	RecordID      string   `json:"recordId,omitempty"`
	RecordIDs     []string `json:"recordIds,omitempty"`
	ConstraintIDs []string `json:"constraintIds,omitempty"`
	TraceID       string   `json:"traceId,omitempty"`
	Summary       string   `json:"summary"`
}

// RequestComparison 描述同一次请求在两套时钟假设下的耗时与等待比例区间。
type RequestComparison struct {
	TraceID          string            `json:"traceId"`
	RootID           string            `json:"rootId"`
	Service          string            `json:"service"`
	Operation        string            `json:"operation"`
	DurationA        DurationBreakdown `json:"durationA"`
	DurationB        DurationBreakdown `json:"durationB"`
	WaitShareA       DurationBreakdown `json:"waitShareA"`
	WaitShareB       DurationBreakdown `json:"waitShareB"`
	OrderUndecidable bool              `json:"orderUndecidable"`
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
	A                  ComparisonSide      `json:"a"`
	B                  ComparisonSide      `json:"b"`
	DurationDeltaMS    float64             `json:"durationDeltaMs"`
	ExecutionDelta     DurationBreakdown   `json:"executionDelta"`
	WaitDelta          DurationBreakdown   `json:"waitDelta"`
	Attribution        string              `json:"attribution"`
	AssumptionChange   string              `json:"assumptionChange,omitempty"`
	Requests           []RequestComparison `json:"requests,omitempty"`
	RankingNotes       []string            `json:"rankingNotes,omitempty"`
	EvidenceIncomplete bool                `json:"evidenceIncomplete"`
	Notes              []string            `json:"notes"`
}
