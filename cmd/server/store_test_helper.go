package main

import "performance-investigation/internal/store"

func newStoreForTest(dir string) *store.Store { return store.New(dir) }
