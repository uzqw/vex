package persistence

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// mockSnapshotter is a controllable Snapshotter for scheduler tests.
type mockSnapshotter struct {
	saveCount atomic.Int64
	saveErr   error
}

func (m *mockSnapshotter) Save(_ context.Context) error {
	m.saveCount.Add(1)
	return m.saveErr
}

func (m *mockSnapshotter) Load(_ context.Context) error { return nil }

func (m *mockSnapshotter) GetLastSnapshotInfo() (*SnapshotInfo, error) { return nil, nil }

// ---- NewScheduler -----------------------------------------------------------

func TestNewScheduler(t *testing.T) {
	snap := &mockSnapshotter{}
	stats := NewStats()
	s := NewScheduler(100*time.Millisecond, snap, stats)
	if s == nil {
		t.Fatal("NewScheduler returned nil")
	}
}

// ---- Start / Stop lifecycle --------------------------------------------------

func TestSchedulerStartStop(t *testing.T) {
	snap := &mockSnapshotter{}
	stats := NewStats()
	s := NewScheduler(50*time.Millisecond, snap, stats)

	ctx := context.Background()
	s.Start(ctx)

	// Wait for at least one tick
	time.Sleep(120 * time.Millisecond)
	s.Stop()

	count := snap.saveCount.Load()
	if count < 1 {
		t.Errorf("expected >= 1 snapshot after tick, got %d", count)
	}
}

func TestSchedulerStartIdempotent(t *testing.T) {
	snap := &mockSnapshotter{}
	stats := NewStats()
	s := NewScheduler(10*time.Second, snap, stats) // long interval, no ticks

	ctx := context.Background()
	s.Start(ctx)
	s.Start(ctx) // second call must not panic or double-start
	s.Stop()
}

func TestSchedulerStopIdempotent(t *testing.T) {
	snap := &mockSnapshotter{}
	stats := NewStats()
	s := NewScheduler(10*time.Second, snap, stats)

	// Stop without Start must not panic
	s.Stop()
	s.Stop()
}

// ---- triggerSnapshot --------------------------------------------------------

func TestTriggerSnapshotSuccess(t *testing.T) {
	snap := &mockSnapshotter{}
	stats := NewStats()
	s := NewScheduler(10*time.Second, snap, stats)

	s.triggerSnapshot(context.Background())

	if stats.TotalSnapshots != 1 {
		t.Errorf("TotalSnapshots = %d, want 1", stats.TotalSnapshots)
	}
	if stats.SnapshotErrors != 0 {
		t.Errorf("SnapshotErrors = %d, want 0", stats.SnapshotErrors)
	}
	if stats.LastError != nil {
		t.Errorf("LastError = %v, want nil", stats.LastError)
	}
}

func TestTriggerSnapshotFailure(t *testing.T) {
	snap := &mockSnapshotter{saveErr: errors.New("disk full")}
	stats := NewStats()
	s := NewScheduler(10*time.Second, snap, stats)

	s.triggerSnapshot(context.Background())

	if stats.SnapshotErrors != 1 {
		t.Errorf("SnapshotErrors = %d, want 1", stats.SnapshotErrors)
	}
	if stats.LastError == nil {
		t.Error("LastError should be set on failure")
	}
	if stats.TotalSnapshots != 0 {
		t.Errorf("TotalSnapshots = %d, want 0", stats.TotalSnapshots)
	}
}

func TestTriggerSnapshotSkipsWhenInProgress(t *testing.T) {
	snap := &mockSnapshotter{}
	stats := NewStats()
	stats.SnapshotInProgress = true // simulate concurrent snapshot
	s := NewScheduler(10*time.Second, snap, stats)

	s.triggerSnapshot(context.Background())

	// Save must not have been called
	if snap.saveCount.Load() != 0 {
		t.Errorf("Save called %d times, want 0 (should skip)", snap.saveCount.Load())
	}
}

func TestTriggerSnapshotUpdatesTimestamps(t *testing.T) {
	snap := &mockSnapshotter{}
	stats := NewStats()
	s := NewScheduler(10*time.Second, snap, stats)

	before := time.Now()
	s.triggerSnapshot(context.Background())
	after := time.Now()

	if stats.LastSnapshotTime.Before(before) || stats.LastSnapshotTime.After(after) {
		t.Errorf("LastSnapshotTime %v not in [%v, %v]", stats.LastSnapshotTime, before, after)
	}
	if stats.LastSnapshotDuration < 0 {
		t.Errorf("LastSnapshotDuration = %v, want >= 0", stats.LastSnapshotDuration)
	}
}
