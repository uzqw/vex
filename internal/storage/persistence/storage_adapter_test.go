package persistence

import (
	"context"
	"errors"
	"testing"
)

// ---- mockStorageInterface ---------------------------------------------------

type mockStorage struct {
	data map[string][]float32
	dim  int
}

func newMockStorage(dim int) *mockStorage {
	return &mockStorage{data: make(map[string][]float32), dim: dim}
}

func (m *mockStorage) Get(key string) ([]float32, bool) {
	v, ok := m.data[key]
	return v, ok
}

func (m *mockStorage) Set(key string, values []float32) error {
	m.data[key] = values
	return nil
}

func (m *mockStorage) Count() int     { return len(m.data) }
func (m *mockStorage) Dimension() int { return m.dim }
func (m *mockStorage) GetAllKeys() []string {
	keys := make([]string, 0, len(m.data))
	for k := range m.data {
		keys = append(keys, k)
	}
	return keys
}

// errStorage.Set always fails
type errStorage struct{ mockStorage }

func (e *errStorage) Set(_ string, _ []float32) error {
	return errors.New("set failed")
}

// ---- StorageAdapter tests ---------------------------------------------------

func TestNewStorageAdapter(t *testing.T) {
	ms := newMockStorage(4)
	a := NewStorageAdapter(ms)
	if a == nil {
		t.Fatal("NewStorageAdapter returned nil")
	}
}

func TestStorageAdapterGetAllVectors(t *testing.T) {
	ms := newMockStorage(3)
	ms.data["a"] = []float32{1, 2, 3}
	ms.data["b"] = []float32{4, 5, 6}

	a := NewStorageAdapter(ms)
	vecs, err := a.GetAllVectors()
	if err != nil {
		t.Fatalf("GetAllVectors error: %v", err)
	}
	if len(vecs) != 2 {
		t.Errorf("len = %d, want 2", len(vecs))
	}
	// Verify copy semantics
	vecs["a"][0] = 99
	if ms.data["a"][0] == 99 {
		t.Error("GetAllVectors should return copies, not references")
	}
}

func TestStorageAdapterGetAllVectorsSkipDeleted(t *testing.T) {
	// Simulate key present in GetAllKeys but gone by the time Get is called
	ms := newMockStorage(3)
	ms.data["a"] = []float32{1, 2, 3}

	a := NewStorageAdapter(ms)

	// Remove after keys are captured (race won't happen in test, just cover the branch)
	delete(ms.data, "a")

	vecs, err := a.GetAllVectors()
	if err != nil {
		t.Fatalf("GetAllVectors error: %v", err)
	}
	if len(vecs) != 0 {
		t.Errorf("expected 0 vectors when key was deleted, got %d", len(vecs))
	}
}

func TestStorageAdapterSetAllVectors(t *testing.T) {
	ms := newMockStorage(3)
	a := NewStorageAdapter(ms)

	input := map[string][]float32{
		"x": {1, 2, 3},
		"y": {4, 5, 6},
	}
	if err := a.SetAllVectors(input); err != nil {
		t.Fatalf("SetAllVectors error: %v", err)
	}
	if ms.Count() != 2 {
		t.Errorf("Count = %d, want 2", ms.Count())
	}
}

func TestStorageAdapterSetAllVectorsError(t *testing.T) {
	es := &errStorage{mockStorage: *newMockStorage(3)}
	a := NewStorageAdapter(es)

	err := a.SetAllVectors(map[string][]float32{"k": {1, 2, 3}})
	if err == nil {
		t.Error("expected error from SetAllVectors when Set fails")
	}
}

func TestStorageAdapterGetDimension(t *testing.T) {
	ms := newMockStorage(128)
	a := NewStorageAdapter(ms)
	if got := a.GetDimension(); got != 128 {
		t.Errorf("GetDimension = %d, want 128", got)
	}
}

// ---- Config / Validate ------------------------------------------------------

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Enabled {
		t.Error("default config should have Enabled=false")
	}
	if cfg.DataDir == "" {
		t.Error("DataDir should not be empty")
	}
	if cfg.SnapshotSeconds <= 0 {
		t.Errorf("SnapshotSeconds = %d, want > 0", cfg.SnapshotSeconds)
	}
	if cfg.KeepSnapshots < 1 {
		t.Errorf("KeepSnapshots = %d, want >= 1", cfg.KeepSnapshots)
	}
}

func TestConfigValidate(t *testing.T) {
	t.Run("disabled config always valid", func(t *testing.T) {
		cfg := Config{Enabled: false}
		if err := cfg.Validate(); err != nil {
			t.Errorf("disabled config validation error: %v", err)
		}
	})

	t.Run("enabled with empty DataDir invalid", func(t *testing.T) {
		cfg := Config{Enabled: true, DataDir: "", KeepSnapshots: 3}
		if err := cfg.Validate(); err == nil {
			t.Error("expected error for empty DataDir")
		}
	})

	t.Run("enabled with KeepSnapshots=0 invalid", func(t *testing.T) {
		cfg := Config{Enabled: true, DataDir: "/tmp", KeepSnapshots: 0}
		if err := cfg.Validate(); err == nil {
			t.Error("expected error for KeepSnapshots=0")
		}
	})

	t.Run("valid enabled config", func(t *testing.T) {
		cfg := Config{Enabled: true, DataDir: "/tmp", KeepSnapshots: 1}
		if err := cfg.Validate(); err != nil {
			t.Errorf("unexpected validation error: %v", err)
		}
	})
}

// ---- NewStats ---------------------------------------------------------------

func TestNewStats(t *testing.T) {
	s := NewStats()
	if s == nil {
		t.Fatal("NewStats returned nil")
	}
	if s.TotalSnapshots != 0 || s.SnapshotErrors != 0 {
		t.Error("fresh Stats should have zero counters")
	}
}

// ---- Manager ----------------------------------------------------------------

func TestNewManager(t *testing.T) {
	t.Run("invalid config returns error", func(t *testing.T) {
		cfg := Config{Enabled: true, DataDir: "", KeepSnapshots: 3}
		_, err := NewManager(cfg, &mockSnapshotter{})
		if err == nil {
			t.Error("expected error for invalid config")
		}
	})

	t.Run("disabled config succeeds", func(t *testing.T) {
		cfg := Config{Enabled: false}
		m, err := NewManager(cfg, &mockSnapshotter{})
		if err != nil {
			t.Fatalf("NewManager error: %v", err)
		}
		if m == nil {
			t.Fatal("NewManager returned nil manager")
		}
	})

	t.Run("enabled with scheduler", func(t *testing.T) {
		cfg := Config{Enabled: true, DataDir: "/tmp", KeepSnapshots: 1, SnapshotSeconds: 60}
		m, err := NewManager(cfg, &mockSnapshotter{})
		if err != nil {
			t.Fatalf("NewManager error: %v", err)
		}
		if m.scheduler == nil {
			t.Error("expected scheduler to be created")
		}
	})

	t.Run("enabled with SnapshotSeconds=0 no scheduler", func(t *testing.T) {
		cfg := Config{Enabled: true, DataDir: "/tmp", KeepSnapshots: 1, SnapshotSeconds: 0}
		m, err := NewManager(cfg, &mockSnapshotter{})
		if err != nil {
			t.Fatalf("NewManager error: %v", err)
		}
		if m.scheduler != nil {
			t.Error("expected no scheduler when SnapshotSeconds=0")
		}
	})
}

func TestManagerStart(t *testing.T) {
	t.Run("disabled start is no-op", func(t *testing.T) {
		cfg := Config{Enabled: false}
		m, _ := NewManager(cfg, &mockSnapshotter{})
		if err := m.Start(context.Background()); err != nil {
			t.Errorf("Start on disabled manager error: %v", err)
		}
	})

	t.Run("enabled start propagates Load error", func(t *testing.T) {
		loadErr := errors.New("load failed")
		failSnap := &failLoadSnapshotter{err: loadErr}
		cfg := Config{Enabled: true, DataDir: "/tmp", KeepSnapshots: 1, SnapshotSeconds: 0}
		m, _ := NewManager(cfg, failSnap)
		err := m.Start(context.Background())
		if err == nil {
			t.Error("expected Start to return Load error")
		}
	})
}

// failLoadSnapshotter returns an error on Load
type failLoadSnapshotter struct {
	err error
}

func (f *failLoadSnapshotter) Save(_ context.Context) error                { return nil }
func (f *failLoadSnapshotter) Load(_ context.Context) error                { return f.err }
func (f *failLoadSnapshotter) GetLastSnapshotInfo() (*SnapshotInfo, error) { return nil, nil }

func TestManagerStop(t *testing.T) {
	t.Run("stop without scheduler", func(t *testing.T) {
		cfg := Config{Enabled: false}
		m, _ := NewManager(cfg, &mockSnapshotter{})
		if err := m.Stop(context.Background()); err != nil {
			t.Errorf("Stop error: %v", err)
		}
	})
}

func TestManagerTriggerSnapshot(t *testing.T) {
	snap := &mockSnapshotter{}
	cfg := Config{Enabled: false}
	m, _ := NewManager(cfg, snap)

	if err := m.TriggerSnapshot(context.Background()); err != nil {
		t.Fatalf("TriggerSnapshot error: %v", err)
	}
	if snap.saveCount.Load() != 1 {
		t.Errorf("Save called %d times, want 1", snap.saveCount.Load())
	}
}

func TestManagerGetStats(t *testing.T) {
	cfg := Config{Enabled: false}
	m, _ := NewManager(cfg, &mockSnapshotter{})
	stats := m.GetStats()
	if stats == nil {
		t.Fatal("GetStats returned nil")
	}
}
