package snapshot

import (
	"errors"
	"strings"
	"time"
)

type AggregatedSnapshotWriter struct {
	writers []SnapshotWriter
}

func NewAggregatedSnapshotWriter(writers ...SnapshotWriter) *AggregatedSnapshotWriter {
	return &AggregatedSnapshotWriter{writers: writers}
}

func (w *AggregatedSnapshotWriter) WriteCounters(now time.Time, counters map[string]int64) error {
	var errs []string
	for _, w := range w.writers {
		err := w.WriteCounters(now, counters)
		if err != nil {
			errs = append(errs, err.Error())
		}
	}
	if len(errs) == 0 {
		return nil
	} else {
		return errors.New("AggregatedSnapshotWriter.WriteCounters:" + strings.Join(errs, ";"))
	}
}
