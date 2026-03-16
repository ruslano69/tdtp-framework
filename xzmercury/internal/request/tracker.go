// Package request tracks the lifecycle of xzmercury key-binding requests.
//
// Each bind call creates a Request record stored in Pipeline Redis with a TTL.
// State transitions are published to Redis Pub/Sub so tdtpcli and any web UI
// can observe progress without polling.
//
// State machine:
//
//	approved  → (key retrieved) → consumed
//	rejected  — terminal
//	expired   — handled implicitly by Redis TTL
package request

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	keyPrefix    = "request:"
	pubsubChannel = "xzmercury:events"
	defaultTTL   = 24 * time.Hour
)

// State represents the lifecycle state of a request.
type State string

const (
	StateApproved State = "approved"
	StateRejected State = "rejected"
	StateConsumed State = "consumed"
)

// Request is the record stored in Pipeline Redis.
type Request struct {
	ID           string    `json:"id"`
	PackageUUID  string    `json:"package_uuid"`
	PipelineName string    `json:"pipeline_name"`
	Caller       string    `json:"caller"`
	State        State     `json:"state"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// Event is the Pub/Sub message published on state changes.
type Event struct {
	RequestID    string    `json:"request_id"`
	PackageUUID  string    `json:"package_uuid"`
	PipelineName string    `json:"pipeline_name"`
	State        State     `json:"state"`
	Timestamp    time.Time `json:"timestamp"`
}

// Tracker stores and publishes request state in Pipeline Redis.
type Tracker struct {
	rdb *redis.Client
	ttl time.Duration
}

// New creates a Tracker backed by Pipeline Redis.
func New(rdb *redis.Client) *Tracker {
	return &Tracker{rdb: rdb, ttl: defaultTTL}
}

// Create persists a new approved request and publishes a Pub/Sub event.
func (t *Tracker) Create(ctx context.Context, packageUUID, pipelineName, caller string) (*Request, error) {
	id, err := newID()
	if err != nil {
		return nil, fmt.Errorf("request: generate id: %w", err)
	}

	now := time.Now().UTC()
	req := &Request{
		ID:           id,
		PackageUUID:  packageUUID,
		PipelineName: pipelineName,
		Caller:       caller,
		State:        StateApproved,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := t.save(ctx, req); err != nil {
		return nil, err
	}
	t.publish(ctx, req)
	return req, nil
}

// Reject creates a terminal rejected request record.
func (t *Tracker) Reject(ctx context.Context, packageUUID, pipelineName, caller string) (*Request, error) {
	id, err := newID()
	if err != nil {
		return nil, fmt.Errorf("request: generate id: %w", err)
	}
	now := time.Now().UTC()
	req := &Request{
		ID:           id,
		PackageUUID:  packageUUID,
		PipelineName: pipelineName,
		Caller:       caller,
		State:        StateRejected,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := t.save(ctx, req); err != nil {
		return nil, err
	}
	t.publish(ctx, req)
	return req, nil
}

// MarkConsumed transitions an existing request to StateConsumed.
// Called from the retrieve handler after a successful GETDEL.
func (t *Tracker) MarkConsumed(ctx context.Context, requestID string) error {
	key := keyPrefix + requestID
	data, err := t.rdb.Get(ctx, key).Bytes()
	if err != nil {
		return fmt.Errorf("request: get %q: %w", requestID, err)
	}
	var req Request
	if err := json.Unmarshal(data, &req); err != nil {
		return fmt.Errorf("request: unmarshal: %w", err)
	}
	req.State = StateConsumed
	req.UpdatedAt = time.Now().UTC()
	if err := t.save(ctx, &req); err != nil {
		return err
	}
	t.publish(ctx, &req)
	return nil
}

// Get retrieves a request by ID.
func (t *Tracker) Get(ctx context.Context, id string) (*Request, error) {
	data, err := t.rdb.Get(ctx, keyPrefix+id).Bytes()
	if err != nil {
		return nil, fmt.Errorf("request: get %q: %w", id, err)
	}
	var req Request
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, err
	}
	return &req, nil
}

func (t *Tracker) save(ctx context.Context, req *Request) error {
	data, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("request: marshal: %w", err)
	}
	return t.rdb.Set(ctx, keyPrefix+req.ID, data, t.ttl).Err()
}

func (t *Tracker) publish(ctx context.Context, req *Request) {
	ev := Event{
		RequestID:    req.ID,
		PackageUUID:  req.PackageUUID,
		PipelineName: req.PipelineName,
		State:        req.State,
		Timestamp:    req.UpdatedAt,
	}
	data, _ := json.Marshal(ev)
	// best-effort; ignore publish errors
	_ = t.rdb.Publish(ctx, pubsubChannel, data).Err()
}

func newID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
