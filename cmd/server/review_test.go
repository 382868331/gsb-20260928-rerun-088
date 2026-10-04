package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func saveInvestigationForTest(t *testing.T, server *api, payload string) string {
	t.Helper()
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/investigations", strings.NewReader(payload)))
	if response.Code != http.StatusOK {
		t.Fatalf("save investigation status = %d: %s", response.Code, response.Body.String())
	}
	var inv struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &inv); err != nil {
		t.Fatal(err)
	}
	return inv.ID
}

func exportBundleForTest(t *testing.T, server *api, invID, query string) map[string]any {
	t.Helper()
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/investigations/"+invID+"/review-bundle"+query, nil))
	if response.Code != http.StatusOK {
		t.Fatalf("export bundle status = %d: %s", response.Code, response.Body.String())
	}
	var bundle map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &bundle); err != nil {
		t.Fatal(err)
	}
	return bundle
}

func importBundleForTest(t *testing.T, server *api, bundle map[string]any) map[string]any {
	t.Helper()
	body, err := json.Marshal(bundle)
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/review-bundles/import", bytes.NewReader(body)))
	if response.Code != http.StatusOK {
		t.Fatalf("import bundle status = %d: %s", response.Code, response.Body.String())
	}
	var result map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func clockOf(t *testing.T, section map[string]any) map[string]any {
	t.Helper()
	clock, ok := section["clock"].(map[string]any)
	if !ok {
		t.Fatalf("missing clock report in %v", section)
	}
	return clock
}

func TestReviewBundleRoundTrip(t *testing.T) {
	source := &api{store: newStoreForTest(t.TempDir())}
	batchID := importFixture(t, source)
	invID := saveInvestigationForTest(t, source, `{"batchId":"`+batchID+`","name":"假设A","rootId":"root","traceId":"trace","hypothesis":{"name":"对时假设A","constraints":[{"id":"c1","type":"offset","anchor":"root","target":"remote","minMs":150,"maxMs":250,"rationale":"网关日志"}]}}`)

	bundle := exportBundleForTest(t, source, invID, "")
	if bundle["format"] != "review-bundle-v1" {
		t.Fatalf("format = %v", bundle["format"])
	}
	saved := clockOf(t, bundle["savedConclusions"].(map[string]any))
	if saved["feasible"] != true {
		t.Fatalf("saved conclusions must be feasible, got %v", saved)
	}
	if got := saved["constraintIds"].([]any); len(got) != 1 || got[0] != "c1" {
		t.Fatalf("enabled constraint ids = %v", got)
	}
	if _, ok := bundle["batch"].(map[string]any)["records"].([]any); !ok {
		t.Fatal("bundle must embed raw records")
	}

	// 另一台同版本应用：全新空存储导入同一复核包。
	replica := &api{store: newStoreForTest(t.TempDir())}
	result := importBundleForTest(t, replica, bundle)
	if problems := result["problems"].([]any); len(problems) != 0 {
		t.Fatalf("unexpected problems: %v", problems)
	}
	recomputed := clockOf(t, result["recomputed"].(map[string]any))
	if recomputed["feasible"] != true {
		t.Fatalf("recomputed must be feasible, got %v", recomputed)
	}
	// 原保存结论与重新求解结果并列返回，且一致。
	if _, ok := result["savedConclusions"].(map[string]any); !ok {
		t.Fatal("saved conclusions must be echoed back")
	}
	if got := recomputed["constraintIds"].([]any); len(got) != 1 || got[0] != "c1" {
		t.Fatalf("recomputed constraint ids = %v", got)
	}
	// 导入后调查与批次已持久化，可按原依据重新打开。
	reopen := httptest.NewRecorder()
	replica.Handler().ServeHTTP(reopen, httptest.NewRequest(http.MethodGet, "/api/investigations/"+invID, nil))
	if reopen.Code != http.StatusOK {
		t.Fatalf("reopen on replica status = %d", reopen.Code)
	}
}

func TestReviewBundleMutuallyExclusiveConstraints(t *testing.T) {
	source := &api{store: newStoreForTest(t.TempDir())}
	batchID := importFixture(t, source)
	// 互斥约束：同一端点的两个时差范围不可能同时成立。
	invID := saveInvestigationForTest(t, source, `{"batchId":"`+batchID+`","name":"互斥","rootId":"root","traceId":"trace","hypothesis":{"constraints":[
		{"id":"c1","type":"offset","anchor":"root","target":"remote","minMs":150,"maxMs":250},
		{"id":"c2","type":"offset","anchor":"root","target":"remote","minMs":400,"maxMs":500}
	]}}`)

	bundle := exportBundleForTest(t, source, invID, "")
	saved := clockOf(t, bundle["savedConclusions"].(map[string]any))
	if saved["feasible"] != false {
		t.Fatalf("mutually exclusive constraints must be infeasible, got %v", saved)
	}
	conflicts := saved["conflicts"].([]any)
	if len(conflicts) == 0 {
		t.Fatal("conflict chain must be saved in the bundle")
	}
	first := conflicts[0].(map[string]any)
	if got := first["constraintIds"].([]any); len(got) != 2 {
		t.Fatalf("conflict chain must name both constraints, got %v", got)
	}
	if got := first["recordIds"].([]any); len(got) == 0 {
		t.Fatal("conflict chain must name involved records")
	}

	replica := &api{store: newStoreForTest(t.TempDir())}
	result := importBundleForTest(t, replica, bundle)
	recomputed := clockOf(t, result["recomputed"].(map[string]any))
	if recomputed["feasible"] != false {
		t.Fatalf("recomputed must reproduce infeasibility, got %v", recomputed)
	}
	if len(recomputed["conflicts"].([]any)) == 0 {
		t.Fatal("recomputed conflict chain must be preserved")
	}
}

func TestReviewBundleDisabledConstraintFeasible(t *testing.T) {
	source := &api{store: newStoreForTest(t.TempDir())}
	batchID := importFixture(t, source)
	// 停用其中一条互斥约束后假设可行，停用状态随复核包往返。
	invID := saveInvestigationForTest(t, source, `{"batchId":"`+batchID+`","name":"停用后可行","rootId":"root","traceId":"trace","hypothesis":{"constraints":[
		{"id":"c1","type":"offset","anchor":"root","target":"remote","minMs":150,"maxMs":250},
		{"id":"c2","type":"offset","anchor":"root","target":"remote","minMs":400,"maxMs":500,"disabled":true}
	]}}`)

	bundle := exportBundleForTest(t, source, invID, "")
	saved := clockOf(t, bundle["savedConclusions"].(map[string]any))
	if saved["feasible"] != true {
		t.Fatalf("disabling one conflicting constraint must be feasible, got %v", saved)
	}
	if got := saved["disabledConstraintIds"].([]any); len(got) != 1 || got[0] != "c2" {
		t.Fatalf("disabled ids = %v", got)
	}

	replica := &api{store: newStoreForTest(t.TempDir())}
	result := importBundleForTest(t, replica, bundle)
	recomputed := clockOf(t, result["recomputed"].(map[string]any))
	if recomputed["feasible"] != true {
		t.Fatalf("recomputed must stay feasible, got %v", recomputed)
	}
	if got := recomputed["disabledConstraintIds"].([]any); len(got) != 1 || got[0] != "c2" {
		t.Fatalf("disabled state must survive round trip, got %v", got)
	}
}

func TestReviewBundleComparisonRoundTrip(t *testing.T) {
	source := &api{store: newStoreForTest(t.TempDir())}
	batchID := importFixture(t, source)
	// 相同原始资料、两套不同假设。
	invA := saveInvestigationForTest(t, source, `{"batchId":"`+batchID+`","name":"假设A","rootId":"root","traceId":"trace","hypothesis":{"constraints":[{"id":"c1","type":"offset","anchor":"root","target":"remote","minMs":150,"maxMs":250}]}}`)
	invB := saveInvestigationForTest(t, source, `{"batchId":"`+batchID+`","name":"假设B","rootId":"root","traceId":"trace","hypothesis":{"constraints":[{"id":"c1","type":"offset","anchor":"root","target":"remote","minMs":100,"maxMs":600}]}}`)

	bundle := exportBundleForTest(t, source, invA, "?compareWith="+invB)
	savedComparison, ok := bundle["comparison"].(map[string]any)
	if !ok {
		t.Fatal("bundle must embed comparison conclusion")
	}
	if savedComparison["assumptionChange"] != true {
		t.Fatalf("same records with different hypotheses must flag assumption change, got %v", savedComparison)
	}
	if related := bundle["relatedInvestigations"].([]any); len(related) != 1 {
		t.Fatalf("related investigations = %v", related)
	}

	replica := &api{store: newStoreForTest(t.TempDir())}
	result := importBundleForTest(t, replica, bundle)
	if problems := result["problems"].([]any); len(problems) != 0 {
		t.Fatalf("unexpected problems: %v", problems)
	}
	if _, ok := result["savedComparison"].(map[string]any); !ok {
		t.Fatal("saved comparison must be echoed back")
	}
	recomputedComparison, ok := result["recomputedComparison"].(map[string]any)
	if !ok {
		t.Fatal("comparison must be recomputed on import")
	}
	if recomputedComparison["assumptionChange"] != true {
		t.Fatalf("recomputed comparison must flag assumption change, got %v", recomputedComparison)
	}
	if recomputedComparison["durationDeltaMs"] != savedComparison["durationDeltaMs"] {
		t.Fatalf("duration delta mismatch: saved %v recomputed %v", savedComparison["durationDeltaMs"], recomputedComparison["durationDeltaMs"])
	}
	// 两次调查都应在导入端持久化。
	for _, id := range []string{invA, invB} {
		reopen := httptest.NewRecorder()
		replica.Handler().ServeHTTP(reopen, httptest.NewRequest(http.MethodGet, "/api/investigations/"+id, nil))
		if reopen.Code != http.StatusOK {
			t.Fatalf("reopen %s on replica status = %d", id, reopen.Code)
		}
	}
}

func TestReviewBundleImportKeepsProblemList(t *testing.T) {
	replica := &api{store: newStoreForTest(t.TempDir())}
	// 手工构造问题包：约束引用缺失记录、记录 ID 重复、约束 ID 重复。
	bundle := map[string]any{
		"format": "review-bundle-v1",
		"investigation": map[string]any{
			"id": "inv-bad", "batchId": "batch-bad", "name": "问题包", "rootId": "root", "traceId": "trace",
			"hypothesis": map[string]any{"constraints": []any{
				map[string]any{"id": "c1", "type": "offset", "anchor": "root", "target": "ghost", "minMs": 1, "maxMs": 2},
				map[string]any{"id": "c1", "type": "offset", "anchor": "root", "target": "remote", "minMs": 1, "maxMs": 2},
			}},
		},
		"batch": map[string]any{
			"id": "batch-bad", "source": "test", "format": "manual-calls-v1",
			"records": []any{
				map[string]any{"id": "root", "traceId": "trace", "service": "api", "operation": "GET", "deviceId": "a", "start": "2026-09-29T10:00:00Z", "end": "2026-09-29T10:00:01Z"},
				map[string]any{"id": "remote", "parentId": "root", "traceId": "trace", "service": "pay", "operation": "auth", "deviceId": "b", "start": "2026-09-29T10:00:00.2Z", "end": "2026-09-29T10:00:00.8Z"},
				map[string]any{"id": "remote", "parentId": "root", "traceId": "trace", "service": "pay", "operation": "auth", "deviceId": "b", "start": "2026-09-29T10:00:00.2Z", "end": "2026-09-29T10:00:00.8Z"},
			},
		},
		"savedConclusions": map[string]any{"clock": map[string]any{"feasible": true}},
	}
	result := importBundleForTest(t, replica, bundle)
	problems := result["problems"].([]any)
	types := map[string]int{}
	for _, item := range problems {
		types[item.(map[string]any)["type"].(string)]++
	}
	if types["missing_record"] == 0 {
		t.Fatalf("missing record must be listed, got %v", problems)
	}
	if types["duplicate_record_id"] == 0 {
		t.Fatalf("duplicate record id must be listed, got %v", problems)
	}
	if types["duplicate_constraint_id"] == 0 {
		t.Fatalf("duplicate constraint id must be listed, got %v", problems)
	}
	// 依据不静默丢弃：引用缺失记录的约束在重算中标记为未知记录冲突。
	recomputed := clockOf(t, result["recomputed"].(map[string]any))
	foundUnknown := false
	for _, item := range recomputed["conflicts"].([]any) {
		if item.(map[string]any)["type"] == "unknown_record" {
			foundUnknown = true
		}
	}
	if !foundUnknown {
		t.Fatalf("dangling constraint must surface as unknown_record conflict, got %v", recomputed)
	}
	// 重复记录保留为原始证据。
	batch := result["recomputed"].(map[string]any)["batch"].(map[string]any)
	if records := batch["records"].([]any); len(records) != 3 {
		t.Fatalf("all raw records must be kept, got %d", len(records))
	}
}

func TestReviewBundleImportRejectsUnknownFormat(t *testing.T) {
	replica := &api{store: newStoreForTest(t.TempDir())}
	response := httptest.NewRecorder()
	replica.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/review-bundles/import", strings.NewReader(`{"format":"other-v9"}`)))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", response.Code)
	}
}
