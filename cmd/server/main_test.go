package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestImportSaveAndReopenRecalculates(t *testing.T) {
	dir := t.TempDir()
	server := &api{store: newStoreForTest(dir)}
	payload := []byte(`{"source":"test","records":[{"id":"root","traceId":"trace","service":"api","operation":"GET","deviceId":"a","start":"2026-09-29T10:00:00Z","end":"2026-09-29T10:00:01Z"},{"id":"child","parentId":"root","traceId":"trace","service":"db","operation":"query","deviceId":"a","start":"2026-09-29T10:00:00.2Z","end":"2026-09-29T10:00:00.8Z"}]}`)
	request := httptest.NewRequest(http.MethodPost, "/api/import/manual", bytes.NewReader(payload))
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("import status = %d: %s", response.Code, response.Body.String())
	}
	var analysis struct {
		Batch struct {
			ID string `json:"id"`
		} `json:"batch"`
		Traces map[string]struct {
			Timing struct {
				WaitMS struct {
					Nominal float64 `json:"nominal"`
				} `json:"waitMs"`
			} `json:"timing"`
		} `json:"traces"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &analysis); err != nil {
		t.Fatal(err)
	}
	if got := analysis.Traces["trace"].Timing.WaitMS.Nominal; got != 600 {
		t.Fatalf("wait = %v, want 600", got)
	}
	saved := []byte(`{"batchId":"` + analysis.Batch.ID + `","name":"smoke","rootId":"root","traceId":"trace","selectedId":"child"}`)
	saveRequest := httptest.NewRequest(http.MethodPost, "/api/investigations", bytes.NewReader(saved))
	saveResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(saveResponse, saveRequest)
	if saveResponse.Code != http.StatusOK {
		t.Fatalf("save status = %d: %s", saveResponse.Code, saveResponse.Body.String())
	}
	var investigation struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(saveResponse.Body.Bytes(), &investigation); err != nil {
		t.Fatal(err)
	}
	reopenRequest := httptest.NewRequest(http.MethodGet, "/api/investigations/"+investigation.ID, nil)
	reopenResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(reopenResponse, reopenRequest)
	if reopenResponse.Code != http.StatusOK {
		t.Fatalf("reopen status = %d: %s", reopenResponse.Code, reopenResponse.Body.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "batches", analysis.Batch.ID+".json")); err != nil {
		t.Fatal(err)
	}
}
func TestCompareEndpointAndReopenConsistency(t *testing.T) {
	dir := t.TempDir()
	server := &api{store: newStoreForTest(dir)}
	payload := []byte(`{"source":"test","records":[
		{"id":"root","traceId":"trace","service":"api","operation":"GET","deviceId":"a","start":"2026-09-29T10:00:00Z","end":"2026-09-29T10:00:01Z"},
		{"id":"child","parentId":"root","traceId":"trace","service":"db","operation":"query","deviceId":"a","start":"2026-09-29T10:00:00.1Z","end":"2026-09-29T10:00:00.7Z"},
		{"id":"child2","parentId":"root","traceId":"trace","service":"db","operation":"query2","deviceId":"a","start":"2026-09-29T10:00:00.3Z","end":"2026-09-29T10:00:00.6Z"},
		{"id":"child","parentId":"root","traceId":"trace","service":"db","operation":"query","deviceId":"a","start":"2026-09-29T10:00:00.1Z","end":"2026-09-29T10:00:00.7Z"},
		{"id":"orphan","parentId":"missing","traceId":"other","service":"w","operation":"retry","deviceId":"b","start":"2026-09-29T10:00:02Z","end":"2026-09-29T10:00:02.3Z"}
	]}`)
	importResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(importResponse, httptest.NewRequest(http.MethodPost, "/api/import/manual", bytes.NewReader(payload)))
	if importResponse.Code != http.StatusOK {
		t.Fatalf("import status = %d: %s", importResponse.Code, importResponse.Body.String())
	}
	var imported struct {
		Batch struct {
			ID string `json:"id"`
		} `json:"batch"`
	}
	if err := json.Unmarshal(importResponse.Body.Bytes(), &imported); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(importResponse.Body.String(), `"children":null`) {
		t.Fatal("analysis JSON must not contain null children")
	}

	save := func(name string) string {
		body := []byte(`{"batchId":"` + imported.Batch.ID + `","name":"` + name + `","rootId":"root","traceId":"trace"}`)
		response := httptest.NewRecorder()
		server.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/investigations", bytes.NewReader(body)))
		if response.Code != http.StatusOK {
			t.Fatalf("save status = %d: %s", response.Code, response.Body.String())
		}
		var inv struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &inv); err != nil {
			t.Fatal(err)
		}
		return inv.ID
	}
	idA, idB := save("第一次"), save("第二次")

	compareResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(compareResponse, httptest.NewRequest(http.MethodGet, "/api/investigations/compare?a="+idA+"&b="+idB, nil))
	if compareResponse.Code != http.StatusOK {
		t.Fatalf("compare status = %d: %s", compareResponse.Code, compareResponse.Body.String())
	}
	var comparison struct {
		DurationDeltaMS float64 `json:"durationDeltaMs"`
		Attribution     string  `json:"attribution"`
	}
	if err := json.Unmarshal(compareResponse.Body.Bytes(), &comparison); err != nil {
		t.Fatal(err)
	}
	if comparison.DurationDeltaMS != 0 || comparison.Attribution != "unchanged" {
		t.Fatalf("same batch comparison = %+v, want unchanged 0", comparison)
	}

	directResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(directResponse, httptest.NewRequest(http.MethodGet, "/api/batches/"+imported.Batch.ID+"/analysis", nil))
	reopenResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(reopenResponse, httptest.NewRequest(http.MethodGet, "/api/investigations/"+idA, nil))
	if reopenResponse.Code != http.StatusOK {
		t.Fatalf("reopen status = %d", reopenResponse.Code)
	}
	var reopened struct {
		Analysis json.RawMessage `json:"analysis"`
	}
	if err := json.Unmarshal(reopenResponse.Body.Bytes(), &reopened); err != nil {
		t.Fatal(err)
	}
	var direct, recomputed map[string]any
	if err := json.Unmarshal(directResponse.Body.Bytes(), &direct); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(reopened.Analysis, &recomputed); err != nil {
		t.Fatal(err)
	}
	delete(direct, "generatedAt")
	delete(recomputed, "generatedAt")
	directJSON, _ := json.Marshal(direct)
	recomputedJSON, _ := json.Marshal(recomputed)
	if string(directJSON) != string(recomputedJSON) {
		t.Fatal("reopened investigation analysis must match recomputed batch analysis")
	}
}
