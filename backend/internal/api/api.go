package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"

	"perfinvest/internal/analysis"
	"perfinvest/internal/ingest"
	"perfinvest/internal/model"
	"perfinvest/internal/store"
)

type Server struct {
	store *store.Store
	mux   *http.ServeMux
}

func NewServer(st *store.Store) *Server {
	s := &Server{store: st, mux: http.NewServeMux()}
	s.routes()
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	s.mux.ServeHTTP(w, r)
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /api/health", s.health)
	s.mux.HandleFunc("GET /api/formats", s.formats)
	s.mux.HandleFunc("POST /api/batches", s.importBatch)
	s.mux.HandleFunc("GET /api/batches", s.listBatches)
	s.mux.HandleFunc("GET /api/traces", s.listTraces)
	s.mux.HandleFunc("GET /api/traces/{traceId}", s.getTrace)
	s.mux.HandleFunc("POST /api/investigations", s.saveInvestigation)
	s.mux.HandleFunc("GET /api/investigations", s.listInvestigations)
	s.mux.HandleFunc("GET /api/investigations/{id}", s.getInvestigation)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("write response: %v", err)
	}
}

func writeErr(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

func newID(prefix string) string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return prefix + "-" + hex.EncodeToString(b)
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) formats(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"formats": ingest.SupportedFormats()})
}

// importBatch accepts either raw file upload (multipart "file") or a JSON body
// {"format":"manual-jsonl","filename":"x.jsonl","content":"..."}.
func (s *Server) importBatch(w http.ResponseWriter, r *http.Request) {
	format, filename, content, err := s.readImport(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	parser, err := ingest.ParserFor(format)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	batchID := newID("batch")
	spans, err := parser.Parse(strings.NewReader(content), batchID)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	batch := model.Batch{
		ID:         batchID,
		Format:     parser.Format(),
		Filename:   filename,
		ImportedAt: time.Now().UTC(),
		SpanCount:  len(spans),
	}
	if err := s.store.SaveBatch(batch, spans); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"batch": batch})
}

func (s *Server) readImport(r *http.Request) (format, filename, content string, err error) {
	ct := r.Header.Get("Content-Type")
	if strings.HasPrefix(ct, "multipart/form-data") {
		if e := r.ParseMultipartForm(16 << 20); e != nil {
			err = e
			return
		}
		format = r.FormValue("format")
		file, fh, e := r.FormFile("file")
		if e != nil {
			err = e
			return
		}
		defer file.Close()
		b, e := io.ReadAll(file)
		if e != nil {
			err = e
			return
		}
		filename = fh.Filename
		content = string(b)
		return
	}
	var body struct {
		Format   string `json:"format"`
		Filename string `json:"filename"`
		Content  string `json:"content"`
	}
	if e := json.NewDecoder(r.Body).Decode(&body); e != nil {
		err = e
		return
	}
	format, filename, content = body.Format, body.Filename, body.Content
	return
}

func (s *Server) listBatches(w http.ResponseWriter, r *http.Request) {
	batches, err := s.store.ListBatches()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if batches == nil {
		batches = []model.Batch{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"batches": batches})
}

// traceSummary is one row in the slow-request list.
type traceSummary struct {
	TraceID    string   `json:"traceId"`
	RootName   string   `json:"rootName"`
	Service    string   `json:"service"`
	StartNS    int64    `json:"startNs"`
	DurationNS int64    `json:"durationNs"`
	SpanCount  int      `json:"spanCount"`
	BatchIDs   []string `json:"batchIds"`
	Flags      []string `json:"flags"`
}

// allSpansForRequest loads spans; ?batch=id repeats to limit scope.
func (s *Server) spansForRequest(r *http.Request) ([]model.Span, error) {
	q := r.URL.Query()
	ids := q["batch"]
	if len(ids) == 0 {
		batches, err := s.store.ListBatches()
		if err != nil {
			return nil, err
		}
		for _, b := range batches {
			ids = append(ids, b.ID)
		}
	}
	return s.store.Spans(ids...)
}

func (s *Server) listTraces(w http.ResponseWriter, r *http.Request) {
	spans, err := s.spansForRequest(r)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	groups := analysis.GroupByTrace(spans)
	ids := make([]string, 0, len(groups))
	for id := range groups {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var summaries []traceSummary
	for _, id := range ids {
		t := analysis.BuildTrace(groups[id], id, r.URL.Query().Get("root"))
		if len(t.Nodes) == 0 {
			continue
		}
		root := t.Nodes[0]
		set := map[string]struct{}{}
		var batches []string
		flagSet := map[string]struct{}{}
		for _, n := range t.Nodes {
			set[n.Span.Source] = struct{}{}
			for _, f := range n.Flags {
				flagSet[string(f)] = struct{}{}
			}
		}
		for b := range set {
			batches = append(batches, b)
		}
		sort.Strings(batches)
		var flags []string
		for f := range flagSet {
			flags = append(flags, f)
		}
		summaries = append(summaries, traceSummary{
			TraceID:    id,
			RootName:   root.Span.Name,
			Service:    root.Span.Service,
			StartNS:    root.Span.StartNS,
			DurationNS: t.RootSummary.DurationNS,
			SpanCount:  len(t.Nodes),
			BatchIDs:   batches,
			Flags:      flags,
		})
	}
	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].DurationNS > summaries[j].DurationNS
	})
	if summaries == nil {
		summaries = []traceSummary{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"traces": summaries})
}

func (s *Server) getTrace(w http.ResponseWriter, r *http.Request) {
	traceID := r.PathValue("traceId")
	spans, err := s.spansForRequest(r)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	groups := analysis.GroupByTrace(spans)
	selected, ok := groups[traceID]
	if !ok {
		writeErr(w, http.StatusNotFound, errNotFound("trace "+traceID))
		return
	}
	t := analysis.BuildTrace(selected, traceID, r.URL.Query().Get("root"))
	writeJSON(w, http.StatusOK, map[string]any{"trace": t})
}

func (s *Server) saveInvestigation(w http.ResponseWriter, r *http.Request) {
	var inv model.Investigation
	if err := json.NewDecoder(r.Body).Decode(&inv); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if inv.TraceID == "" {
		writeErr(w, http.StatusBadRequest, errNotFound("traceId is required"))
		return
	}
	now := time.Now().UTC()
	if inv.ID == "" {
		inv.ID = newID("inv")
		inv.CreatedAt = now
	}
	inv.UpdatedAt = now
	if err := s.store.SaveInvestigation(inv); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	// Reopen immediately: recompute the trace from raw spans so the client
	// gets the reproducible breakdown rather than anything stored.
	spans, err := s.store.Spans(inv.BatchIDs...)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	t := analysis.BuildTrace(spans, inv.TraceID, inv.RootSpanID)
	writeJSON(w, http.StatusCreated, map[string]any{"investigation": inv, "trace": t})
}

func (s *Server) listInvestigations(w http.ResponseWriter, r *http.Request) {
	invs, err := s.store.ListInvestigations()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if invs == nil {
		invs = []model.Investigation{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"investigations": invs})
}

func (s *Server) getInvestigation(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	inv, err := s.store.GetInvestigation(id)
	if err != nil {
		writeErr(w, http.StatusNotFound, errNotFound("investigation "+id))
		return
	}
	spans, err := s.store.Spans(inv.BatchIDs...)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	t := analysis.BuildTrace(spans, inv.TraceID, inv.RootSpanID)
	writeJSON(w, http.StatusOK, map[string]any{"investigation": inv, "trace": t})
}

type httpErr string

func (e httpErr) Error() string    { return string(e) }
func errNotFound(msg string) error { return httpErr(msg) }
