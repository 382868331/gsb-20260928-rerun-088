package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
