package telemetry

import (
	"testing"
	"time"
)

func TestProbeBucketsWeightedRollupQueryAndCleanup(t *testing.T) {
	service := newTestService(t, nil)
	start := time.Unix(1_700_000_000, 0).UTC().Truncate(10 * time.Minute)
	buckets := []ProbeBucket{
		{NodeID: "node", TaskID: "task", BucketStart: start, Attempts: 2, Successes: 1, Failures: 1, LatencyCount: 1, LatencySum: 10, LatencyMin: 10, LatencyMax: 10},
		{NodeID: "node", TaskID: "task", BucketStart: start.Add(time.Minute), Attempts: 2, Successes: 2, LatencyCount: 2, LatencySum: 60, LatencyMin: 20, LatencyMax: 40},
	}
	for _, bucket := range buckets {
		if err := service.SubmitProbe(bucket); err != nil {
			t.Fatal(err)
		}
	}
	syncWrites(t, service)
	if _, err := service.rollupProbe(t.Context(), "probe_1m", "probe_10m", 10*time.Minute, start.Add(10*time.Minute)); err != nil {
		t.Fatal(err)
	}
	points, err := service.QueryProbe(t.Context(), "node", "task", Resolution10m, start, start.Add(10*time.Minute))
	if err != nil || len(points) != 1 {
		t.Fatalf("points=%#v err=%v", points, err)
	}
	point := points[0]
	if point.Attempts != 4 || point.Successes != 3 || point.Failures != 1 || point.LatencyCount != 3 || point.LatencyAverage == nil || *point.LatencyAverage != 70.0/3.0 ||
		point.LatencyMin == nil || *point.LatencyMin != 10 || point.LatencyMax == nil || *point.LatencyMax != 40 || point.SuccessRate != .75 {
		t.Fatalf("weighted point=%#v", point)
	}
	if err := service.DeleteProbeTask(t.Context(), "task"); err != nil {
		t.Fatal(err)
	}
	points, err = service.QueryProbe(t.Context(), "node", "task", Resolution1m, start, start.Add(10*time.Minute))
	if err != nil || len(points) != 0 {
		t.Fatalf("task cleanup points=%#v err=%v", points, err)
	}
}

func TestProbeFailureOnlyBucketHasNullLatencyAndClearRemovesHistory(t *testing.T) {
	service := newTestService(t, nil)
	start := time.Unix(1_700_000_000, 0).UTC().Truncate(time.Minute)
	if err := service.SubmitProbe(ProbeBucket{NodeID: "node", TaskID: "task", BucketStart: start, Attempts: 3, Failures: 3}); err != nil {
		t.Fatal(err)
	}
	syncWrites(t, service)
	points, err := service.QueryProbe(t.Context(), "node", "task", Resolution1m, start, start.Add(time.Minute))
	if err != nil || len(points) != 1 || points[0].LatencyAverage != nil || points[0].LatencyMin != nil || points[0].LatencyMax != nil {
		t.Fatalf("failure-only points=%#v err=%v", points, err)
	}
	if err := service.ClearHistory(t.Context()); err != nil {
		t.Fatal(err)
	}
	points, err = service.QueryProbe(t.Context(), "node", "task", Resolution1m, start, start.Add(time.Minute))
	if err != nil || len(points) != 0 {
		t.Fatalf("clear points=%#v err=%v", points, err)
	}
}

func TestProbeRetentionIsIdempotentAndNonOverlapping(t *testing.T) {
	service := newTestService(t, nil)
	now := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	for _, age := range []time.Duration{30 * time.Minute, 2 * 24 * time.Hour, 10 * 24 * time.Hour, 40 * 24 * time.Hour} {
		if err := service.SubmitProbe(ProbeBucket{NodeID: "node", TaskID: "task", BucketStart: now.Add(-age), Attempts: 1, Failures: 1}); err != nil {
			t.Fatal(err)
		}
	}
	syncWrites(t, service)
	for run := 0; run < 2; run++ {
		if _, err := service.RunMaintenance(t.Context(), now); err != nil {
			t.Fatalf("RunMaintenance(%d): %v", run, err)
		}
	}
	cases := []struct {
		resolution Resolution
		from, to   time.Time
	}{{Resolution1m, now.Add(-time.Hour), now}, {Resolution10m, now.Add(-3 * 24 * time.Hour), now.Add(-24 * time.Hour)}, {Resolution1h, now.Add(-11 * 24 * time.Hour), now.Add(-7 * 24 * time.Hour)}}
	for _, test := range cases {
		points, err := service.QueryProbe(t.Context(), "node", "task", test.resolution, test.from, test.to)
		if err != nil || len(points) != 1 {
			t.Fatalf("%s points=%#v err=%v", test.resolution, points, err)
		}
	}
	for _, table := range []string{"probe_1m", "probe_10m", "probe_1h"} {
		var count int
		if err := service.store.reader.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM "+table).Scan(&count); err != nil || count != 1 {
			t.Fatalf("%s count=%d err=%v; want 1", table, count, err)
		}
	}
}

func TestProbeRetentionAlignsTierBoundaryWhenMaintenanceHasSeconds(t *testing.T) {
	service := newTestService(t, nil)
	now := time.Date(2026, 10, 10, 12, 0, 37, 0, time.UTC)
	coarseStart := now.Add(-DefaultRetention.OneMinute).Truncate(10 * time.Minute)
	hourStart := now.Add(-DefaultRetention.TenMinute).Truncate(time.Hour)
	for _, start := range []time.Time{
		coarseStart.Add(5 * time.Minute), coarseStart.Add(10 * time.Minute),
		hourStart.Add(10 * time.Minute), hourStart.Add(time.Hour),
	} {
		if err := service.SubmitProbe(ProbeBucket{NodeID: "node", TaskID: "task", BucketStart: start, Attempts: 1, Failures: 1}); err != nil {
			t.Fatal(err)
		}
	}
	syncWrites(t, service)
	if _, err := service.RunMaintenance(t.Context(), now); err != nil {
		t.Fatal(err)
	}
	minutePoints, err := service.QueryProbe(t.Context(), "node", "task", Resolution1m, coarseStart, coarseStart.Add(20*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	tenMinutePoints, err := service.QueryProbe(t.Context(), "node", "task", Resolution10m, coarseStart, coarseStart.Add(20*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(minutePoints) != 1 || !minutePoints[0].BucketStart.Equal(coarseStart.Add(10*time.Minute)) {
		t.Fatalf("one-minute tier=%#v; want only the bucket at the aligned boundary", minutePoints)
	}
	if len(tenMinutePoints) != 1 || !tenMinutePoints[0].BucketStart.Equal(coarseStart) {
		t.Fatalf("ten-minute tier=%#v; want only the preceding coarse bucket", tenMinutePoints)
	}
	tenMinutePoints, err = service.QueryProbe(t.Context(), "node", "task", Resolution10m, hourStart, hourStart.Add(2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	hourPoints, err := service.QueryProbe(t.Context(), "node", "task", Resolution1h, hourStart, hourStart.Add(2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(tenMinutePoints) != 1 || !tenMinutePoints[0].BucketStart.Equal(hourStart.Add(time.Hour)) {
		t.Fatalf("ten-minute tier=%#v; want only the bucket at the aligned hour boundary", tenMinutePoints)
	}
	if len(hourPoints) != 1 || !hourPoints[0].BucketStart.Equal(hourStart) {
		t.Fatalf("one-hour tier=%#v; want only the preceding coarse bucket", hourPoints)
	}
}
