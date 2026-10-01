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
	mux.HandleFunc("GET /api/investigations", a.listInvestigations)
	mux.HandleFunc("POST /api/investigations", a.saveInvestigation)
	mux.HandleFunc("GET /api/investigations/compare", a.compareInvestigations)
	mux.HandleFunc("GET /api/investigations/{id}", a.getInvestigation)
	mux.Handle("/", http.FileServer(http.Dir("./web")))
	return mux
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
	writeJSONResponse(w, map[string]any{"investigation": inv, "analysis": analysis.Analyze(batch)})
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
	return inv, analysis.Analyze(batch), nil
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
