package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func importFixture(t *testing.T, server *api) string {
	t.Helper()
	payload := []byte(`{"source":"test","records":[
		{"id":"root","traceId":"trace","service":"api","operation":"GET","deviceId":"a","start":"2026-09-29T10:00:00Z","end":"2026-09-29T10:00:01Z"},
		{"id":"remote","parentId":"root","traceId":"trace","service":"pay","operation":"auth","deviceId":"b","start":"2026-09-29T10:00:00.2Z","end":"2026-09-29T10:00:00.8Z"}
	]}`)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/import/manual", bytes.NewReader(payload)))
	if response.Code != http.StatusOK {
		t.Fatalf("import status = %d: %s", response.Code, response.Body.String())
	}
	var imported struct {
		Batch struct {
			ID string `json:"id"`
		} `json:"batch"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &imported); err != nil {
		t.Fatal(err)
	}
	return imported.Batch.ID
}

func TestClockAnalysisEndpointAndHypothesisPersistence(t *testing.T) {
	server := &api{store: newStoreForTest(t.TempDir())}
	batchID := importFixture(t, server)

	hypothesis := `{"name":"对时假设A","constraints":[{"id":"c1","type":"offset","anchor":"root","target":"remote","minMs":150,"maxMs":250,"rationale":"网关日志"}]}`
	clockResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(clockResponse, httptest.NewRequest(http.MethodPost, "/api/batches/"+batchID+"/clock-analysis", strings.NewReader(hypothesis)))
	if clockResponse.Code != http.StatusOK {
		t.Fatalf("clock-analysis status = %d: %s", clockResponse.Code, clockResponse.Body.String())
	}
	var clockResult struct {
		Clock struct {
			Feasible      bool     `json:"feasible"`
			ConstraintIDs []string `json:"constraintIds"`
		} `json:"clock"`
		Traces map[string]struct {
			Timing struct {
				AssumptionBased bool `json:"assumptionBased"`
				WaitMS          struct {
					Upper float64 `json:"upper"`
				} `json:"waitMs"`
			} `json:"timing"`
		} `json:"traces"`
	}
	if err := json.Unmarshal(clockResponse.Body.Bytes(), &clockResult); err != nil {
		t.Fatal(err)
	}
	if !clockResult.Clock.Feasible || len(clockResult.Clock.ConstraintIDs) != 1 {
		t.Fatalf("expected feasible hypothesis with one constraint, got %+v", clockResult.Clock)
	}
	if !clockResult.Traces["trace"].Timing.AssumptionBased {
		t.Fatal("tightened bounds must be flagged as assumption-based")
	}
	if got := clockResult.Traces["trace"].Timing.WaitMS.Upper; got != 700 {
		t.Fatalf("wait upper = %v, want 700", got)
	}

	// 矛盾假设：同一端点互斥的两个时差范围。
	conflicting := `{"constraints":[
		{"id":"c1","type":"offset","anchor":"root","target":"remote","minMs":150,"maxMs":250},
		{"id":"c2","type":"offset","anchor":"root","target":"remote","minMs":400,"maxMs":500}
	]}`
	conflictResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(conflictResponse, httptest.NewRequest(http.MethodPost, "/api/batches/"+batchID+"/clock-analysis", strings.NewReader(conflicting)))
	var conflictResult struct {
		Clock struct {
			Feasible  bool `json:"feasible"`
			Conflicts []struct {
				Type          string   `json:"type"`
				ConstraintIDs []string `json:"constraintIds"`
				RecordIDs     []string `json:"recordIds"`
			} `json:"conflicts"`
		} `json:"clock"`
	}
	if err := json.Unmarshal(conflictResponse.Body.Bytes(), &conflictResult); err != nil {
		t.Fatal(err)
	}
	if conflictResult.Clock.Feasible {
		t.Fatal("contradictory ranges must be infeasible")
	}
	if len(conflictResult.Clock.Conflicts) == 0 || len(conflictResult.Clock.Conflicts[0].ConstraintIDs) != 2 {
		t.Fatalf("conflict must name both constraints, got %+v", conflictResult.Clock.Conflicts)
	}

	// 假设随调查保存，重开后仍然生效且原始时间戳不变。
	saved := []byte(`{"batchId":"` + batchID + `","name":"带假设","rootId":"root","traceId":"trace","hypothesis":` + hypothesis + `}`)
	saveResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(saveResponse, httptest.NewRequest(http.MethodPost, "/api/investigations", bytes.NewReader(saved)))
	if saveResponse.Code != http.StatusOK {
		t.Fatalf("save status = %d: %s", saveResponse.Code, saveResponse.Body.String())
	}
	var inv struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(saveResponse.Body.Bytes(), &inv); err != nil {
		t.Fatal(err)
	}
	reopenResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(reopenResponse, httptest.NewRequest(http.MethodGet, "/api/investigations/"+inv.ID, nil))
	if reopenResponse.Code != http.StatusOK {
		t.Fatalf("reopen status = %d", reopenResponse.Code)
	}
	var reopened struct {
		Investigation struct {
			Hypothesis struct {
				Name        string `json:"name"`
				Constraints []struct {
					ID string `json:"id"`
				} `json:"constraints"`
			} `json:"hypothesis"`
		} `json:"investigation"`
		Analysis struct {
			Traces map[string]struct {
				Timing struct {
					AssumptionBased bool `json:"assumptionBased"`
				} `json:"timing"`
				Record struct {
					Start string `json:"start"`
				} `json:"record"`
			} `json:"traces"`
		} `json:"analysis"`
	}
	if err := json.Unmarshal(reopenResponse.Body.Bytes(), &reopened); err != nil {
		t.Fatal(err)
	}
	if reopened.Investigation.Hypothesis.Name != "对时假设A" || len(reopened.Investigation.Hypothesis.Constraints) != 1 {
		t.Fatalf("hypothesis must survive save/reopen, got %+v", reopened.Investigation.Hypothesis)
	}
	if !reopened.Analysis.Traces["trace"].Timing.AssumptionBased {
		t.Fatal("reopened investigation must recompute with saved hypothesis")
	}
	if reopened.Analysis.Traces["trace"].Record.Start != "2026-09-29T10:00:00Z" {
		t.Fatalf("raw timestamps must not be rewritten, got %s", reopened.Analysis.Traces["trace"].Record.Start)
	}
}
