package main

import (
	"flag"
	"log"
	"net/http"
	"os"

	"perfinvest/internal/api"
	"perfinvest/internal/store"
)

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	dataDir := flag.String("data", "./data", "directory for batches and investigations")
	staticDir := flag.String("static", "", "directory of built frontend (served at /)")
	flag.Parse()

	st, err := store.Open(*dataDir)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	h := api.NewServer(st)

	mux := http.NewServeMux()
	mux.Handle("/api/", h)
	if *staticDir != "" {
		fs := http.FileServer(http.Dir(*staticDir))
		mux.Handle("/", spaHandler{staticDir: *staticDir, fallback: fs})
	}

	log.Printf("perf-invest listening on %s (data=%s)", *addr, *dataDir)
	if err := http.ListenAndServe(*addr, mux); err != nil {
		log.Fatal(err)
	}
}

// spaHandler serves static files and falls back to index.html for client routes.
type spaHandler struct {
	staticDir string
	fallback  http.Handler
}

func (h spaHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	if path != "/" {
		if _, err := os.Stat(h.staticDir + path); err == nil {
			h.fallback.ServeHTTP(w, r)
			return
		}
		r2 := r.Clone(r.Context())
		r2.URL.Path = "/"
		h.fallback.ServeHTTP(w, r2)
		return
	}
	h.fallback.ServeHTTP(w, r)
}
