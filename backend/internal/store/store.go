// Package store persists import batches, their raw spans and saved
// investigations as plain JSON files. Raw evidence is always preserved;
// derived breakdowns are never stored, so reopening an investigation
// recomputes them deterministically.
package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"perfinvest/internal/model"
)

type Store struct {
	root string
	mu   sync.RWMutex
}

func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(filepath.Join(dir, "batches"), 0o755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(dir, "investigations"), 0o755); err != nil {
		return nil, err
	}
	return &Store{root: dir}, nil
}

// SaveBatch stores a batch together with every raw span.
func (s *Store) SaveBatch(b model.Batch, spans []model.Span) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	dir := filepath.Join(s.root, "batches", b.ID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(dir, "batch.json"), b); err != nil {
		return err
	}
	return writeJSON(filepath.Join(dir, "spans.json"), spans)
}

// ListBatches returns batch metadata, newest first.
func (s *Store) ListBatches() ([]model.Batch, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entries, err := os.ReadDir(filepath.Join(s.root, "batches"))
	if err != nil {
		return nil, err
	}
	var out []model.Batch
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		var b model.Batch
		if err := readJSON(filepath.Join(s.root, "batches", e.Name(), "batch.json"), &b); err != nil {
			continue
		}
		out = append(out, b)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ImportedAt.After(out[j].ImportedAt) })
	return out, nil
}

// Spans returns the raw spans of one or more batches.
func (s *Store) Spans(batchIDs ...string) ([]model.Span, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var all []model.Span
	for _, id := range batchIDs {
		var spans []model.Span
		if err := readJSON(filepath.Join(s.root, "batches", id, "spans.json"), &spans); err != nil {
			return nil, fmt.Errorf("batch %q: %w", id, err)
		}
		all = append(all, spans...)
	}
	return all, nil
}

// SaveInvestigation stores only the user's view/annotations.
func (s *Store) SaveInvestigation(inv model.Investigation) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return writeJSON(filepath.Join(s.root, "investigations", inv.ID+".json"), inv)
}

// GetInvestigation loads a saved view.
func (s *Store) GetInvestigation(id string) (*model.Investigation, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var inv model.Investigation
	if err := readJSON(filepath.Join(s.root, "investigations", id+".json"), &inv); err != nil {
		return nil, err
	}
	return &inv, nil
}

// ListInvestigations returns saved views, newest first.
func (s *Store) ListInvestigations() ([]model.Investigation, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entries, err := os.ReadDir(filepath.Join(s.root, "investigations"))
	if err != nil {
		return nil, err
	}
	var out []model.Investigation
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		var inv model.Investigation
		if err := readJSON(filepath.Join(s.root, "investigations", e.Name()), &inv); err != nil {
			continue
		}
		out = append(out, inv)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return out, nil
}

func writeJSON(path string, v any) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func readJSON(path string, v any) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return json.NewDecoder(f).Decode(v)
}
