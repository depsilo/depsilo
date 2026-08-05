package upstream

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"depsilo/internal/db"
)

func TestConcurrentProbePersistenceStaysBelowSlowSQLThreshold(t *testing.T) {
	const upstreamCount = 27

	database := bootstrapDB(t)
	records := make([]db.UpstreamRecord, 0, upstreamCount)
	for index := 0; index < upstreamCount; index++ {
		records = append(records, db.UpstreamRecord{
			AdapterType:   "pypi",
			Name:          fmt.Sprintf("source-%02d", index),
			URL:           fmt.Sprintf("https://source-%02d.example", index),
			Priority:      index + 1,
			ProbeMode:     "active",
			ProbeInterval: "30m",
			Healthy:       true,
			SuccessRate:   1,
		})
	}
	if err := database.Create(&records).Error; err != nil {
		t.Fatal(err)
	}
	pool, err := NewPoolFromRecords(records)
	if err != nil {
		t.Fatal(err)
	}

	type persistenceResult struct {
		duration time.Duration
		err      error
	}
	start := make(chan struct{})
	results := make(chan persistenceResult, upstreamCount)
	var workers sync.WaitGroup
	checkedAt := time.Now().UTC()
	for _, candidate := range pool.Snapshot() {
		upstream := candidate
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			result := ProbeResult{
				Healthy:   true,
				Latency:   100 * time.Millisecond,
				CheckedAt: checkedAt,
			}
			upstream.applyProbe(result)
			startedAt := time.Now()
			err := persistProbe(context.Background(), database, upstream, result)
			results <- persistenceResult{duration: time.Since(startedAt), err: err}
		}()
	}
	close(start)
	workers.Wait()
	close(results)

	var maximum time.Duration
	for result := range results {
		if result.err != nil {
			t.Errorf("persist probe: %v", result.err)
		}
		if result.duration > maximum {
			maximum = result.duration
		}
	}
	if maximum >= concurrentProbePersistenceBudget {
		t.Errorf(
			"concurrent probe persistence crossed test budget: max=%s budget=%s",
			maximum,
			concurrentProbePersistenceBudget,
		)
	}

	var latencyLogs int64
	if err := database.Model(&db.UpstreamLatencyLog{}).Count(&latencyLogs).Error; err != nil {
		t.Fatal(err)
	}
	if latencyLogs != upstreamCount {
		t.Errorf("latency logs=%d want=%d", latencyLogs, upstreamCount)
	}
	var stored []db.UpstreamRecord
	if err := database.Order("id").Find(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if len(stored) != upstreamCount {
		t.Fatalf("upstream records=%d want=%d", len(stored), upstreamCount)
	}
	for _, record := range stored {
		if record.LastCheckedAt.IsZero() || !record.Healthy || record.AvgLatencyMs != 100 {
			t.Errorf("upstream %d was not fully updated: %#v", record.ID, record)
		}
	}
	t.Logf("maximum concurrent probe persistence latency: %s", maximum)
}
