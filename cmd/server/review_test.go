package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const reviewFixture = `{"source":"test","records":[
	{"id":"root","traceId":"trace","service":"api","operation":"GET","deviceId":"a","start":"2026-09-29T10:00:00Z","end":"2026-09-29T10:00:01Z"},
	{"id":"remote","parentId":"root","traceId":"trace","service":"pay","operation":"auth","deviceId":"b","start":"2026-09-29T10:00:00.2Z","end":"2026-09-29T10:00:00.8Z"}
]}`

func importReviewFixture(t *testing.T, server *api) string {
	t.Helper()
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/import/manual", bytes.NewReader([]byte(reviewFixture))))
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

func saveReviewInvestigation(t *testing.T, server *api, batchID, name, hypothesisJSON string) string {
	t.Helper()
	body := `{"batchId":"` + batchID + `","name":"` + name + `","rootId":"root","traceId":"trace"`
	if hypothesisJSON != "" {
		body += `,"hypothesis":` + hypothesisJSON
	}
	body += `}`
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/investigations", bytes.NewReader([]byte(body))))
	if response.Code != http.StatusOK {
		t.Fatalf("save status = %d: %s", response.Code, response.Body.String())
	}
	var saved struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &saved); err != nil {
		t.Fatal(err)
	}
	return saved.ID
}

func postJSON(t *testing.T, server *api, path string, body []byte) map[string]any {
	t.Helper()
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body)))
	if response.Code != http.StatusOK && response.Code != http.StatusCreated {
		t.Fatalf("%s status = %d: %s", path, response.Code, response.Body.String())
	}
	var result map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	return result
}

// 互斥约束复核包导出后，在另一台同版本应用（独立数据目录）导入：
// 按原依据重算仍不可行，原保存结论与重算结果一致，问题清单保留冲突链。
func TestReviewPackageContradictionRoundTripAcrossInstances(t *testing.T) {
	source := &api{store: newStoreForTest(t.TempDir())}
	batchID := importReviewFixture(t, source)
	hypothesis := `{"name":"互斥","constraints":[
		{"id":"c1","type":"offset","anchor":"root","target":"remote","minMs":150,"maxMs":250},
		{"id":"c2","type":"offset","anchor":"root","target":"remote","minMs":400,"maxMs":500}
	]}`
	invID := saveReviewInvestigation(t, source, batchID, "互斥假设", hypothesis)

	pkg := postJSON(t, source, "/api/review/export", []byte(`{"investigationIds":["`+invID+`"]}`))
	if pkg["format"] != "investigation-review-v1" {
		t.Fatalf("unexpected format %v", pkg["format"])
	}
	rawPackage, _ := json.Marshal(pkg)

	target := &api{store: newStoreForTest(t.TempDir())}
	imported := postJSON(t, target, "/api/review/import", rawPackage)
	recomputed := imported["recomputed"].(map[string]any)
	if recomputed["conclusionMatch"] != true {
		t.Fatalf("infeasible conclusion must reproduce on same-version instance: %+v", recomputed["notes"])
	}
	entries := recomputed["entries"].([]any)
	clock := entries[0].(map[string]any)["clock"].(map[string]any)
	if clock["feasible"] != false {
		t.Fatal("recomputed hypothesis must stay infeasible")
	}
	issues := imported["issues"].([]any)
	foundContradiction := false
	for _, item := range issues {
		issue := item.(map[string]any)
		if issue["type"] == "contradiction" {
			ids := issue["constraintIds"].([]any)
			if len(ids) != 2 {
				t.Fatalf("conflict chain must name both constraints, got %v", ids)
			}
			foundContradiction = true
		}
	}
	if !foundContradiction {
		t.Fatalf("import must keep conflict chain in issue list: %+v", issues)
	}
	// 原保存结论必须与重新求解结果分开存放。
	saved := imported["package"].(map[string]any)["savedConclusions"].([]any)
	if saved[0].(map[string]any)["clock"] == nil {
		t.Fatal("sealed saved conclusion must travel separately from recomputed result")
	}
}

// 停用互斥依据后可行：停用状态随包往返；且相同原始资料不同假设的对照结论可重算。
func TestReviewPackageDisabledAndComparisonRoundTrip(t *testing.T) {
	source := &api{store: newStoreForTest(t.TempDir())}
	batchID := importReviewFixture(t, source)
	idWide := saveReviewInvestigation(t, source, batchID, "宽假设", "")
	tight := `{"name":"紧假设","constraints":[
		{"id":"c1","type":"offset","anchor":"root","target":"remote","minMs":150,"maxMs":250},
		{"id":"c2","type":"offset","anchor":"root","target":"remote","minMs":400,"maxMs":500,"disabled":true}
	]}`
	idTight := saveReviewInvestigation(t, source, batchID, "紧假设", tight)

	pkg := postJSON(t, source, "/api/review/export", []byte(`{"a":"`+idWide+`","b":"`+idTight+`"}`))
	if comparison, ok := pkg["comparison"].(map[string]any); !ok || comparison["assumptionChange"] != true {
		t.Fatalf("sealed comparison must record assumption change, got %+v", pkg["comparison"])
	}
	rawPackage, _ := json.Marshal(pkg)

	target := &api{store: newStoreForTest(t.TempDir())}
	imported := postJSON(t, target, "/api/review/import", rawPackage)
	recomputed := imported["recomputed"].(map[string]any)
	if recomputed["conclusionMatch"] != true {
		t.Fatalf("saved and recomputed must match after disabling: %+v", recomputed["notes"])
	}
	if match, ok := recomputed["comparisonMatch"].(bool); !ok || !match {
		t.Fatalf("comparison conclusion must reproduce, got %v", recomputed["comparisonMatch"])
	}
	for _, entry := range recomputed["entries"].([]any) {
		e := entry.(map[string]any)
		clock := e["clock"].(map[string]any)
		disabled, _ := clock["disabledConstraintIds"].([]any)
		if e["investigationId"] == idTight {
			if clock["feasible"] != true || len(disabled) != 1 {
				t.Fatalf("disabled constraint must survive import and keep hypothesis feasible: %+v", clock)
			}
		}
	}
	if len(imported["issues"].([]any)) != 0 {
		t.Fatalf("clean package must import without issues: %+v", imported["issues"])
	}
}

// 缺失记录与重复 ID 都进入问题清单，且重复 ID 不覆盖本机已有数据。
func TestReviewPackageImportKeepsIssueList(t *testing.T) {
	source := &api{store: newStoreForTest(t.TempDir())}
	batchID := importReviewFixture(t, source)
	hypothesis := `{"constraints":[{"id":"c1","type":"offset","anchor":"root","target":"ghost","minMs":10,"maxMs":200}]}`
	invID := saveReviewInvestigation(t, source, batchID, "缺记录", hypothesis)
	pkg := postJSON(t, source, "/api/review/export", []byte(`{"investigationIds":["`+invID+`"]}`))
	rawPackage, _ := json.Marshal(pkg)

	target := &api{store: newStoreForTest(t.TempDir())}
	imported := postJSON(t, target, "/api/review/import", rawPackage)
	issues := imported["issues"].([]any)
	types := map[string]int{}
	for _, item := range issues {
		types[item.(map[string]any)["type"].(string)]++
	}
	if types["missing_record"] == 0 || types["unknown_record"] == 0 {
		t.Fatalf("missing record must be listed by audit and solver: %v", types)
	}

	// 再次导入同一包：ID 冲突要显式告警，不能静默覆盖。
	second := postJSON(t, target, "/api/review/import", rawPackage)
	warnings := 0
	for _, item := range second["issues"].([]any) {
		issue := item.(map[string]any)
		if issue["severity"] == "warning" && strings.HasSuffix(issue["type"].(string), "_exists") {
			warnings++
		}
	}
	if warnings < 2 {
		t.Fatalf("re-import must warn about existing batch and investigation, got %+v", second["issues"])
	}
}
