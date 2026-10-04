package main

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"performance-investigation/internal/analysis"
	"performance-investigation/internal/ingest"
	"performance-investigation/internal/model"
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
	mux.HandleFunc("GET /api/investigations/{id}/review-bundle", a.exportReviewBundle)
	mux.HandleFunc("POST /api/review-bundles/import", a.importReviewBundle)
	mux.Handle("/", http.FileServer(http.Dir("./web")))
	return mux
}

var errUnknownBundleFormat = errors.New("复核包格式不支持，需要 review-bundle-v1")

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

// exportReviewBundle 导出调查复核包：原始记录、启用与停用的时钟依据、
// 冲突链与调查对照结论；可选 compareWith 指定同批次的另一次调查生成对照结论。
func (a *api) exportReviewBundle(w http.ResponseWriter, r *http.Request) {
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
	related := []model.Investigation{}
	var comparison *model.Comparison
	if otherID := r.URL.Query().Get("compareWith"); otherID != "" {
		invA, analysisA, err := a.loadInvestigation(inv.ID)
		if err != nil {
			writeError(w, http.StatusNotFound, err)
			return
		}
		invB, analysisB, err := a.loadInvestigation(otherID)
		if err != nil {
			writeError(w, http.StatusNotFound, err)
			return
		}
		result, err := analysis.Compare(analysisA, analysisB, invA, invB)
		if err != nil {
			writeError(w, http.StatusUnprocessableEntity, err)
			return
		}
		comparison = &result
		related = append(related, invB)
	}
	writeJSONResponse(w, analysis.BuildReviewBundle(inv, batch, related, comparison))
}

// importReviewBundle 导入复核包：按原依据重算，原保存结论与重新求解结果
// 并列返回；缺失记录或重复 ID 列入问题清单，依据不静默丢弃。
func (a *api) importReviewBundle(w http.ResponseWriter, r *http.Request) {
	var bundle model.ReviewBundle
	if err := jsonDecode(r, &bundle); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if bundle.Format != model.ReviewBundleFormat {
		writeError(w, http.StatusBadRequest, errUnknownBundleFormat)
		return
	}
	problems := analysis.ValidateReviewBundle(bundle)
	if err := a.store.SaveBatch(bundle.Batch); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if err := a.store.SaveInvestigation(bundle.Investigation); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	for _, related := range bundle.RelatedInvestigations {
		if err := a.store.SaveInvestigation(related); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
	}
	recomputed := analysis.AnalyzeWithClock(bundle.Batch, bundle.Investigation.Hypothesis)
	result := model.ReviewImportResult{
		Investigation:    bundle.Investigation,
		BatchID:          bundle.Batch.ID,
		Problems:         problems,
		SavedConclusions: bundle.SavedConclusions,
		Recomputed:       recomputed,
	}
	if bundle.Comparison != nil {
		result.SavedComparison = bundle.Comparison
		if len(bundle.RelatedInvestigations) > 0 {
			related := bundle.RelatedInvestigations[0]
			relatedAnalysis := analysis.AnalyzeWithClock(bundle.Batch, related.Hypothesis)
			if recomputedComparison, err := analysis.Compare(recomputed, relatedAnalysis, bundle.Investigation, related); err == nil {
				result.RecomputedComparison = &recomputedComparison
			} else {
				result.Problems = append(result.Problems, model.ReviewProblem{
					Type:    model.ProblemComparisonNotRecomputed,
					ID:      related.ID,
					Summary: "对照结论无法按原依据重算：" + err.Error(),
				})
			}
		}
	}
	writeJSONResponse(w, result)
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
