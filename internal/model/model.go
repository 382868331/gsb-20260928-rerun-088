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
	WaitMS        DurationBreakdown `json:"waitMs"`
	ExecutionMS   DurationBreakdown `json:"executionMs"`
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
