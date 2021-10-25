package snapshot

import (
	"encoding/json"
	"sync"
	"time"
)

type MemorySnapshotWriter struct {
	counters  map[string]int64
	updatedAt time.Time
	lock      sync.RWMutex
}

func NewMemorySnapshotWriter() *MemorySnapshotWriter {
	return &MemorySnapshotWriter{counters: make(map[string]int64)}
}

func (w *MemorySnapshotWriter) WriteCounters(now time.Time, counters map[string]int64) error {
	w.lock.Lock()
	defer w.lock.Unlock()
	w.counters = counters
	w.updatedAt = now
	return nil
}

func (w *MemorySnapshotWriter) Dump() ([]byte, error) {
	w.lock.RLock()
	defer w.lock.RUnlock()

	payload := struct {
		Counters  map[string]int64
		UpdatedAt time.Time
	}{
		Counters:  w.counters,
		UpdatedAt: w.updatedAt,
	}

	return json.Marshal(payload)
}
