package request

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func newTestTracker(t *testing.T) *Tracker {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	return New(rdb)
}

// --- Create ---

func TestCreate_StoresApprovedRequest(t *testing.T) {
	tracker := newTestTracker(t)

	req, err := tracker.Create(context.Background(), "uuid-1", "salary-pipeline", "svc_tdtp")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if req.ID == "" {
		t.Error("Create() вернул пустой ID")
	}
	if req.State != StateApproved {
		t.Errorf("Create() State = %q, want %q", req.State, StateApproved)
	}
	if req.PackageUUID != "uuid-1" {
		t.Errorf("Create() PackageUUID = %q, want %q", req.PackageUUID, "uuid-1")
	}
	if req.PipelineName != "salary-pipeline" {
		t.Errorf("Create() PipelineName = %q, want %q", req.PipelineName, "salary-pipeline")
	}
	if req.Caller != "svc_tdtp" {
		t.Errorf("Create() Caller = %q, want %q", req.Caller, "svc_tdtp")
	}
}

func TestCreate_CreatedAtIsRecent(t *testing.T) {
	tracker := newTestTracker(t)
	before := time.Now().UTC().Add(-time.Second)

	req, err := tracker.Create(context.Background(), "uuid", "pipeline", "caller")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if req.CreatedAt.Before(before) {
		t.Error("Create() CreatedAt в прошлом")
	}
}

func TestCreate_UniqueIDs(t *testing.T) {
	tracker := newTestTracker(t)
	ctx := context.Background()

	r1, _ := tracker.Create(ctx, "uuid-1", "pipeline", "caller")
	r2, _ := tracker.Create(ctx, "uuid-2", "pipeline", "caller")

	if r1.ID == r2.ID {
		t.Error("Create() вернул одинаковые ID для разных запросов")
	}
}

// --- Reject ---

func TestReject_StoresRejectedRequest(t *testing.T) {
	tracker := newTestTracker(t)

	req, err := tracker.Reject(context.Background(), "uuid-1", "pipeline", "unauthorized-user")
	if err != nil {
		t.Fatalf("Reject() error = %v", err)
	}
	if req.State != StateRejected {
		t.Errorf("Reject() State = %q, want %q", req.State, StateRejected)
	}
	if req.ID == "" {
		t.Error("Reject() вернул пустой ID")
	}
}

// --- Get ---

func TestGet_ReturnsStoredRequest(t *testing.T) {
	tracker := newTestTracker(t)
	ctx := context.Background()

	created, err := tracker.Create(ctx, "uuid-1", "salary-pipeline", "svc_tdtp")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	got, err := tracker.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.ID != created.ID {
		t.Errorf("Get().ID = %q, want %q", got.ID, created.ID)
	}
	if got.State != StateApproved {
		t.Errorf("Get().State = %q, want %q", got.State, StateApproved)
	}
	if got.PackageUUID != created.PackageUUID {
		t.Errorf("Get().PackageUUID = %q, want %q", got.PackageUUID, created.PackageUUID)
	}
}

func TestGet_NotFound(t *testing.T) {
	tracker := newTestTracker(t)

	_, err := tracker.Get(context.Background(), "nonexistent-id")
	if err == nil {
		t.Error("Get() expected error for nonexistent ID")
	}
}

// --- MarkConsumed ---

func TestMarkConsumed_TransitionsToConsumed(t *testing.T) {
	tracker := newTestTracker(t)
	ctx := context.Background()

	req, _ := tracker.Create(ctx, "uuid-1", "pipeline", "caller")

	if err := tracker.MarkConsumed(ctx, req.ID); err != nil {
		t.Fatalf("MarkConsumed() error = %v", err)
	}

	got, err := tracker.Get(ctx, req.ID)
	if err != nil {
		t.Fatalf("Get() after MarkConsumed error = %v", err)
	}
	if got.State != StateConsumed {
		t.Errorf("после MarkConsumed() State = %q, want %q", got.State, StateConsumed)
	}
}

func TestMarkConsumed_UpdatedAtAdvances(t *testing.T) {
	tracker := newTestTracker(t)
	ctx := context.Background()

	req, _ := tracker.Create(ctx, "uuid-1", "pipeline", "caller")
	time.Sleep(time.Millisecond) // гарантируем что время изменится

	_ = tracker.MarkConsumed(ctx, req.ID)

	got, _ := tracker.Get(ctx, req.ID)
	if !got.UpdatedAt.After(req.CreatedAt) {
		t.Error("MarkConsumed() должен обновить UpdatedAt")
	}
}

func TestMarkConsumed_NotFound(t *testing.T) {
	tracker := newTestTracker(t)

	err := tracker.MarkConsumed(context.Background(), "nonexistent-id")
	if err == nil {
		t.Error("MarkConsumed() expected error для несуществующего ID")
	}
}

// --- Полный жизненный цикл ---

func TestLifecycle_ApprovedToConsumed(t *testing.T) {
	tracker := newTestTracker(t)
	ctx := context.Background()

	// approved
	req, _ := tracker.Create(ctx, "uuid-lifecycle", "salary-pipeline", "svc_tdtp")
	if req.State != StateApproved {
		t.Fatalf("State после Create = %q, want approved", req.State)
	}

	// consumed
	_ = tracker.MarkConsumed(ctx, req.ID)
	got, _ := tracker.Get(ctx, req.ID)
	if got.State != StateConsumed {
		t.Errorf("State после MarkConsumed = %q, want consumed", got.State)
	}
}
