package snapshot

import (
	"context"
	"time"

	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
	"github.com/influxdata/influxdb-client-go/v2/api"
)

type InfluxDBSnapshotWriter struct {
	writer api.WriteAPIBlocking
	tags   map[string]string
}

func NewInfluxDBSnapshotWriter(serverURL, authToken, org, bucket string, tags map[string]string) *InfluxDBSnapshotWriter {
	client := influxdb2.NewClient(serverURL, authToken)
	writer := client.WriteAPIBlocking(org, bucket)
	return &InfluxDBSnapshotWriter{
		writer: writer,
		tags:   tags,
	}
}

func (w *InfluxDBSnapshotWriter) WriteCounters(now time.Time, counters map[string]int64) error {
	fields := make(map[string]interface{})
	for k, v := range counters {
		fields[k] = v
	}

	p := influxdb2.NewPoint("job_stat", w.tags, fields, now)
	return w.writer.WritePoint(context.Background(), p)
}
