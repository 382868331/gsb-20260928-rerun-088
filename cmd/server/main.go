package main

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"performance-investigation/internal/analysis"
	"performance-investigation/internal/ingest"
	"performance-investigation/internal/model"
	"performance-investigation/internal/review"
	"performance-investigation/internal/store"
)

func main() {
	dataDir := os.Getenv("DATA_DIR")
	if dataDir == "" {
		dataDir = "./data"
	}
	server := &api{store: store.New(dataDir)}
	mux := server.Handler()
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Fatal(http.ListenAndServe(":"+port, mux))
}

type api struct {
	store *store.Store
}

var errMissingCompareIDs = errors.New("需要提供 a 和 b 两个调查 ID 进行对照")

type reviewExportRequest struct {
	InvestigationIDs []string `json:"investigationIds"`
	A                string   `json:"a"`
	B                string   `json:"b"`
}

func (a *api) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/batches", a.listBatches)
	mux.HandleFunc("POST /api/import/manual", a.importManual)
	mux.HandleFunc("GET /api/batches/{id}/analysis", a.batchAnalysis)
	mux.HandleFunc("POST /api/batches/{id}/clock-analysis", a.clockAnalysis)
	mux.HandleFunc("GET /api/investigations", a.listInvestigations)
	mux.HandleFunc("POST /api/investigations", a.saveInvestigation)
	mux.HandleFunc("GET /api/investigations/compare", a.compareInvestigations)
	mux.HandleFunc("GET /api/investigations/{id}", a.getInvestigation)
	mux.HandleFunc("POST /api/review/export", a.exportReview)
	mux.HandleFunc("POST /api/review/import", a.importReview)
	mux.Handle("/", http.FileServer(http.Dir("./web")))
	return mux
}

// exportReview 封存复核包：原始记录、启用/停用时钟依据、冲突链与对照结论。
func (a *api) exportReview(w http.ResponseWriter, r *http.Request) {
	var request reviewExportRequest
	if err := jsonDecode(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	ids := request.InvestigationIDs
	if len(ids) == 0 && (request.A != "" || request.B != "") {
		ids = []string{request.A, request.B}
	}
	ids = nonEmptyUnique(ids)
	if len(ids) == 0 {
		writeError(w, http.StatusBadRequest, errors.New("需要指定至少一个调查 ID 导出复核包"))
		return
	}
	if len(ids) > 2 {
		writeError(w, http.StatusBadRequest, errors.New("复核包最多包含两次调查及其对照结论"))
		return
	}
	invs := make([]model.Investigation, 0, len(ids))
	results := make([]model.Analysis, 0, len(ids))
	var batch model.Batch
	for _, id := range ids {
		inv, result, err := a.loadInvestigation(id)
		if err != nil {
			writeError(w, http.StatusNotFound, err)
			return
		}
		if len(invs) == 0 {
			stored, err := a.store.GetBatch(inv.BatchID)
			if err != nil {
				writeError(w, http.StatusNotFound, err)
				return
			}
			batch = stored
		} else if inv.BatchID != batch.ID {
			writeError(w, http.StatusBadRequest, errors.New("复核包中的调查必须基于同一批次原始记录"))
			return
		}
		invs = append(invs, inv)
		results = append(results, result)
	}
	var comparison *model.Comparison
	if len(invs) == 2 {
		compared, err := analysis.Compare(results[0], results[1], invs[0], invs[1])
		if err != nil {
			writeError(w, http.StatusUnprocessableEntity, err)
			return
		}
		comparison = &compared
	}
	writeJSONResponse(w, review.Build(batch, invs, results, comparison, time.Now().UTC()))
}

// importReview 导入复核包：按包内原始依据重新求解，原保存结论与重算结果分开返回。
func (a *api) importReview(w http.ResponseWriter, r *http.Request) {
	var pkg model.ReviewPackage
	if err := jsonDecode(r, &pkg); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if pkg.Format != model.ReviewPackageFormat {
		writeError(w, http.StatusBadRequest, fmt.Errorf("不支持的复核包格式 %q，期望 %s", pkg.Format, model.ReviewPackageFormat))
		return
	}
	if len(pkg.Batch.Records) == 0 {
		writeError(w, http.StatusBadRequest, errors.New("复核包缺少原始记录"))
		return
	}
	summary, issues := review.Recompute(pkg)
	if _, err := a.store.GetBatch(pkg.Batch.ID); err == nil {
		issues = append(issues, model.ReviewIssue{Severity: model.ReviewIssueWarning, Type: "batch_id_exists", RecordIDs: []string{pkg.Batch.ID}, Summary: fmt.Sprintf("批次 %s 已存在：未覆盖本机数据，重算仍基于包内原始记录", pkg.Batch.ID)})
	} else if err := a.store.SaveBatch(pkg.Batch); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	for _, inv := range pkg.Investigations {
		if _, err := a.store.GetInvestigation(inv.ID); err == nil {
			issues = append(issues, model.ReviewIssue{Severity: model.ReviewIssueWarning, Type: "investigation_id_exists", Investigation: inv.ID, Summary: fmt.Sprintf("调查 %s 已存在：未覆盖本机保存的调查", inv.ID)})
			continue
		}
		if err := a.store.SaveInvestigation(inv); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
	}
	summary.Notes = append(summary.Notes, issueNoteCount(issues))
	writeJSONResponse(w, model.ReviewImportResult{Package: pkg, Recomputed: summary, Issues: issues})
}

func nonEmptyUnique(values []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func issueNoteCount(issues []model.ReviewIssue) string {
	errors, warnings := 0, 0
	for _, issue := range issues {
		if issue.Severity == model.ReviewIssueError {
			errors++
		} else {
			warnings++
		}
	}
	return fmt.Sprintf("问题清单保留 %d 个错误、%d 个警告，依据未被默默丢弃", errors, warnings)
}

func (a *api) listBatches(w http.ResponseWriter, _ *http.Request) {
	items, err := a.store.ListBatches()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSONResponse(w, items)
}

func (a *api) importManual(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	batch, err := ingest.Manual(body)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := a.store.SaveBatch(batch); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSONResponse(w, analysis.Analyze(batch))
}

// clockAnalysis 用请求体中的时钟假设对批次原始记录做约束分析：
// 返回可行解释、矛盾约束链及被假设收紧的区间，原始记录保持不变。
func (a *api) clockAnalysis(w http.ResponseWriter, r *http.Request) {
	batch, err := a.store.GetBatch(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	var hypothesis model.ClockHypothesis
	if err := jsonDecode(r, &hypothesis); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSONResponse(w, analysis.AnalyzeWithClock(batch, hypothesis))
}

func (a *api) batchAnalysis(w http.ResponseWriter, r *http.Request) {
	batch, err := a.store.GetBatch(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	writeJSONResponse(w, analysis.Analyze(batch))
}

func (a *api) listInvestigations(w http.ResponseWriter, _ *http.Request) {
	items, err := a.store.ListInvestigations()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSONResponse(w, items)
}

func (a *api) saveInvestigation(w http.ResponseWriter, r *http.Request) {
	var inv model.Investigation
	if err := jsonDecode(r, &inv); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if inv.ID == "" {
		inv.ID = newID("inv")
	}
	now := time.Now().UTC()
	if inv.CreatedAt.IsZero() {
		inv.CreatedAt = now
	}
	inv.UpdatedAt = now
	if inv.View.TimeMode == "" {
		inv.View.TimeMode = "bounds"
	}
	if err := a.store.SaveInvestigation(inv); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSONResponse(w, inv)
}

func (a *api) getInvestigation(w http.ResponseWriter, r *http.Request) {
	inv, err := a.store.GetInvestigation(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	batch, err := a.store.GetBatch(inv.BatchID)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	writeJSONResponse(w, map[string]any{"investigation": inv, "analysis": analysis.AnalyzeWithClock(batch, inv.Hypothesis)})
}

func (a *api) compareInvestigations(w http.ResponseWriter, r *http.Request) {
	idA, idB := r.URL.Query().Get("a"), r.URL.Query().Get("b")
	if idA == "" || idB == "" {
		writeError(w, http.StatusBadRequest, errMissingCompareIDs)
		return
	}
	invA, analysisA, err := a.loadInvestigation(idA)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	invB, analysisB, err := a.loadInvestigation(idB)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	comparison, err := analysis.Compare(analysisA, analysisB, invA, invB)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err)
		return
	}
	writeJSONResponse(w, comparison)
}

func (a *api) loadInvestigation(id string) (model.Investigation, model.Analysis, error) {
	inv, err := a.store.GetInvestigation(id)
	if err != nil {
		return model.Investigation{}, model.Analysis{}, err
	}
	batch, err := a.store.GetBatch(inv.BatchID)
	if err != nil {
		return model.Investigation{}, model.Analysis{}, err
	}
	return inv, analysis.AnalyzeWithClock(batch, inv.Hypothesis), nil
}

func jsonDecode(r *http.Request, target any) error {
	decoder := newJSONDecoder(r.Body)
	return decoder.Decode(target)
}

func writeJSONResponse(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = newJSONEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, err error) {
	w.WriteHeader(status)
	_ = newJSONEncoder(w).Encode(map[string]string{"error": err.Error()})
}

func newID(prefix string) string {
	bytes := make([]byte, 8)
	_, _ = rand.Read(bytes)
	return prefix + "-" + hex.EncodeToString(bytes)
}
