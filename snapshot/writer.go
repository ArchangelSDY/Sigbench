package snapshot

import "time"

type SnapshotWriter interface {
	WriteCounters(now time.Time, counters map[string]int64) error
}
