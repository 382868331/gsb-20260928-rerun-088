package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"perfinvest/internal/store"
)

const sample = `
{"id":"r","traceId":"t-1","name":"POST /order","service":"api","startMs":0,"endMs":10000}
{"id":"a","parentId":"r","traceId":"t-1","name":"db.stock","service":"db","startMs":1000,"endMs":7000}
{"id":"b","parentId":"r","traceId":"t-1","name":"http.charge","service":"pay","startMs":2000,"endMs":8000}
{"id":"x","parentId":"ghost","traceId":"t-1","name":"orphan","startMs":0,"endMs":3000}
`

func newTestServer(t *testing.T) (*Server, string) {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	return NewServer(st), dir
}

func TestImportReconstructSaveReopen(t *testing.T) {
	s, _ := newTestServer(t)
	body, _ := json.Marshal(map[string]string{
		"format":   "manual-jsonl",
		"filename": "sample.jsonl",
		"content":  sample,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/batches", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("import: %d %s", rec.Code, rec.Body.String())
	}
	var imp struct {
		Batch struct {
			ID string `json:"id"`
		} `json:"batch"`
	}
	mustDecode(t, rec.Body.Bytes(), &imp)

	// list traces
	rec = get(t, s, "/api/traces")
	var tl struct {
		Traces []struct {
			TraceID    string   `json:"traceId"`
			DurationNS int64    `json:"durationNs"`
			Flags      []string `json:"flags"`
		} `json:"traces"`
	}
	mustDecode(t, rec.Body.Bytes(), &tl)
	if len(tl.Traces) != 1 || tl.Traces[0].DurationNS != 10_000_000_000 {
		t.Fatalf("trace list wrong: %+v", tl.Traces)
	}
	var sawMissing bool
	for _, f := range tl.Traces[0].Flags {
		if f == "missing_parent" {
			sawMissing = true
		}
	}
	if !sawMissing {
		t.Fatalf("missing_parent flag should surface in trace summary")
	}

	// get trace: parallel children union [1,7)+[2,8) = 7s wait
	rec = get(t, s, "/api/traces/t-1")
	var tr struct {
		Trace struct {
			Roots []struct {
				Breakdown struct {
					WaitingNS   int64 `json:"waitingNs"`
					ExecutingNS int64 `json:"executingNs"`
					OverlapNS   int64 `json:"overlapNs"`
				} `json:"breakdown"`
			} `json:"roots"`
		} `json:"trace"`
	}
	mustDecode(t, rec.Body.Bytes(), &tr)
	rootBD := tr.Trace.Roots[0].Breakdown
	if rootBD.WaitingNS != 7_000_000_000 || rootBD.ExecutingNS != 3_000_000_000 {
		t.Fatalf("root breakdown: %+v", rootBD)
	}
	if rootBD.OverlapNS != 12_000_000_000 {
		t.Fatalf("raw overlap should expose 5s parallel double count: %+v", rootBD)
	}

	// save investigation
	invBody, _ := json.Marshal(map[string]any{
		"title":    "checkout slow",
		"traceId":  "t-1",
		"batchIds": []string{imp.Batch.ID},
		"notes":    "waiting on downstream",
	})
	req = httptest.NewRequest(http.MethodPost, "/api/investigations", bytes.NewReader(invBody))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("save: %d %s", rec.Code, rec.Body.String())
	}
	var saved struct {
		Investigation struct {
			ID string `json:"id"`
		} `json:"investigation"`
	}
	mustDecode(t, rec.Body.Bytes(), &saved)

	// reopen: breakdown must be recomputed from raw spans
	rec = get(t, s, "/api/investigations/"+saved.Investigation.ID)
	var reopened struct {
		Trace struct {
			Roots []struct {
				Breakdown struct {
					WaitingNS  int64 `json:"waitingNs"`
					Recomputed bool  `json:"recomputed"`
				} `json:"breakdown"`
			} `json:"roots"`
		} `json:"trace"`
	}
	mustDecode(t, rec.Body.Bytes(), &reopened)
	if reopened.Trace.Roots[0].Breakdown.WaitingNS != 7_000_000_000 || !reopened.Trace.Roots[0].Breakdown.Recomputed {
		t.Fatalf("reopened breakdown not recomputed: %+v", reopened.Trace.Roots[0].Breakdown)
	}
}

func TestBadFormatRejected(t *testing.T) {
	s, _ := newTestServer(t)
	body, _ := json.Marshal(map[string]string{"format": "zip", "content": "x"})
	req := httptest.NewRequest(http.MethodPost, "/api/batches", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "unsupported") {
		t.Fatalf("expected 400 unsupported format, got %d %s", rec.Code, rec.Body.String())
	}
}

func get(t *testing.T, s *Server, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func mustDecode(t *testing.T, b []byte, v any) {
	t.Helper()
	if err := json.Unmarshal(b, v); err != nil {
		t.Fatalf("decode %s: %v", string(b), err)
	}
}
