package model

import "time"

// Span is a single manual call record (the raw evidence).
// Times are stored as nanoseconds since the Unix epoch exactly as imported;
// clock bias across devices is never "corrected" by rewriting them.
type Span struct {
	ID       string `json:"id"`
	ParentID string `json:"parentId,omitempty"`
	TraceID  string `json:"traceId,omitempty"`
	Service  string `json:"service,omitempty"`
	Name     string `json:"name"`
	Kind     string `json:"kind,omitempty"` // server | client | internal | ...
	StartNS  int64  `json:"startNs"`
	EndNS    int64  `json:"endNs"`
	Device   string `json:"device,omitempty"`
	Source   string `json:"source"`        // batch id this record came from
	Raw      any    `json:"raw,omitempty"` // original record, verbatim
	Seq      int    `json:"seq"`           // order inside the source file
}

func (s Span) DurationNS() int64 { return s.EndNS - s.StartNS }

// Batch is an import of one collection of raw records.
type Batch struct {
	ID         string    `json:"id"`
	Format     string    `json:"format"`
	Filename   string    `json:"filename,omitempty"`
	ImportedAt time.Time `json:"importedAt"`
	SpanCount  int       `json:"spanCount"`
	Note       string    `json:"note,omitempty"`
}

// NodeFlag marks structural problems with a span in a reconstructed tree.
type NodeFlag string

const (
	FlagMissingParent NodeFlag = "missing_parent"
	FlagDuplicate     NodeFlag = "duplicate"
	FlagClockAnomaly  NodeFlag = "clock_anomaly"
)

// Breakdown explains where a span's elapsed time went.
// All fields are nanoseconds. Children are merged by their overlap with the
// parent's own interval, so parallel siblings are never simply summed.
type Breakdown struct {
	ExecutingNS    int64 `json:"executingNs"`
	WaitingNS      int64 `json:"waitingNs"`
	OverlapNS      int64 `json:"overlapNs"`
	OutsideNS      int64 `json:"outsideNs"` // child time falling outside parent window (clock bias)
	UnknownNS      int64 `json:"unknownNs"` // zero/negative duration cannot be explained
	DurationNS     int64 `json:"durationNs"`
	Recomputed     bool  `json:"recomputed"`
	ClockUncertain bool  `json:"clockUncertain"`
}

// Node is one span as it appears in a reconstructed trace.
type Node struct {
	Span      Span       `json:"span"`
	Depth     int        `json:"depth"`
	Children  []*Node    `json:"children"`
	Flags     []NodeFlag `json:"flags,omitempty"`
	Breakdown Breakdown  `json:"breakdown"`
	Dups      []Span     `json:"duplicates,omitempty"` // later records sharing the same id
}

// Trace is a reconstructed causal view for one request.
type Trace struct {
	TraceID    string   `json:"traceId"`
	Roots      []*Node  `json:"roots"`
	Nodes      []*Node  `json:"nodes"` // pre-order traversal
	RootSpanID string   `json:"rootSpanId,omitempty"`
	Notes      []string `json:"notes,omitempty"`
	// RootSummary aggregates time across root spans (e.g. inferred roots).
	RootSummary *Breakdown `json:"rootSummary,omitempty"`
}

// Investigation is a saved analysis view. Only annotations/user data are
// stored; the breakdown is recomputed from raw spans every time the view
// is reopened, so it stays reproducible.
type Investigation struct {
	ID         string    `json:"id"`
	Title      string    `json:"title"`
	TraceID    string    `json:"traceId"`
	BatchIDs   []string  `json:"batchIds"`
	RootSpanID string    `json:"rootSpanId,omitempty"`
	Notes      string    `json:"notes,omitempty"`
	Tags       []string  `json:"tags,omitempty"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}
