package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"performance-investigation/internal/model"
)

type Store struct {
	root string
	mu   sync.RWMutex
}

func New(root string) *Store { return &Store{root: root} }

func (s *Store) SaveBatch(batch model.Batch) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(filepath.Join(s.root, "batches"), 0o755); err != nil {
		return err
	}
	return writeJSON(filepath.Join(s.root, "batches", batch.ID+".json"), batch)
}

func (s *Store) GetBatch(id string) (model.Batch, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var batch model.Batch
	err := readJSON(filepath.Join(s.root, "batches", id+".json"), &batch)
	return batch, err
}

func (s *Store) ListBatches() ([]model.Batch, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	files, err := filepath.Glob(filepath.Join(s.root, "batches", "*.json"))
	if err != nil {
		return nil, err
	}
	batches := make([]model.Batch, 0, len(files))
	for _, file := range files {
		var batch model.Batch
		if err := readJSON(file, &batch); err == nil {
			batches = append(batches, batch)
		}
	}
	return batches, nil
}

func (s *Store) SaveInvestigation(inv model.Investigation) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(filepath.Join(s.root, "investigations"), 0o755); err != nil {
		return err
	}
	return writeJSON(filepath.Join(s.root, "investigations", inv.ID+".json"), inv)
}

func (s *Store) GetInvestigation(id string) (model.Investigation, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var inv model.Investigation
	err := readJSON(filepath.Join(s.root, "investigations", id+".json"), &inv)
	return inv, err
}

func (s *Store) ListInvestigations() ([]model.Investigation, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	files, err := filepath.Glob(filepath.Join(s.root, "investigations", "*.json"))
	if err != nil {
		return nil, err
	}
	items := make([]model.Investigation, 0, len(files))
	for _, file := range files {
		var item model.Investigation
		if err := readJSON(file, &item); err == nil {
			items = append(items, item)
		}
	}
	return items, nil
}

func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func readJSON(path string, target any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, target)
}
