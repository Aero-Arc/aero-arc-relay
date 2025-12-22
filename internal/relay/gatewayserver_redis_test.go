package relay

import (
	"context"
	"sync"
	"testing"
	"time"

	agentv1 "github.com/aero-arc/aero-arc-protos/gen/go/aeroarc/agent/v1"
	relayv1 "github.com/aero-arc/aero-arc-protos/gen/go/aeroarc/relay/v1"
)

type fakeRoutingStore struct {
	mu    sync.Mutex
	calls []routingCall
	ch    chan struct{}
}

type routingCall struct {
	droneID   string
	relayID   string
	sessionID string
	ttl       time.Duration
}

func (f *fakeRoutingStore) UpsertDroneRouting(ctx context.Context, droneID, relayID, sessionID string, ttl time.Duration) error {
	f.mu.Lock()
	f.calls = append(f.calls, routingCall{
		droneID:   droneID,
		relayID:   relayID,
		sessionID: sessionID,
		ttl:       ttl,
	})
	f.mu.Unlock()
	select {
	case f.ch <- struct{}{}:
	default:
	}
	return nil
}

func (f *fakeRoutingStore) DeleteDroneRouting(ctx context.Context, droneID string) error {
	// no-op for tests
	return nil
}

func (f *fakeRoutingStore) lastSessionID() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.calls) == 0 {
		return ""
	}
	return f.calls[len(f.calls)-1].sessionID
}

func TestRegister_PublishesRedisRoutingAndRefreshesTTL(t *testing.T) {
	store := &fakeRoutingStore{ch: make(chan struct{}, 10)}
	r := &Relay{
		redisRoutingStore:             store,
		redisRoutingTTL:               50 * time.Millisecond,
		redisRoutingCancelBySessionID: make(map[string]context.CancelFunc),
		grpcSessions:                  make(map[string]*relayv1.DroneStatus),
		sessionByID:                   make(map[string]*relayv1.DroneStatus),
	}

	resp, err := r.Register(context.Background(), &agentv1.RegisterRequest{AgentId: "drone-1"})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}

	// Expect an immediate publish.
	select {
	case <-store.ch:
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("expected routing publish")
	}

	// Expect at least one refresh tick.
	select {
	case <-store.ch:
	case <-time.After(300 * time.Millisecond):
		t.Fatalf("expected routing refresh publish")
	}

	// Stop refreshing and ensure no more calls arrive.
	r.removeSessionByID(resp.GetSessionId())
	last := store.lastSessionID()
	time.Sleep(150 * time.Millisecond)
	if got := store.lastSessionID(); got != last {
		t.Fatalf("expected no further publishes after session removal; last=%q got=%q", last, got)
	}
}

func TestRegister_ReconnectCancelsOldSessionRefreshAndUpdatesSessionID(t *testing.T) {
	store := &fakeRoutingStore{ch: make(chan struct{}, 50)}
	r := &Relay{
		redisRoutingStore:             store,
		redisRoutingTTL:               40 * time.Millisecond,
		redisRoutingCancelBySessionID: make(map[string]context.CancelFunc),
		grpcSessions:                  make(map[string]*relayv1.DroneStatus),
		sessionByID:                   make(map[string]*relayv1.DroneStatus),
	}

	first, err := r.Register(context.Background(), &agentv1.RegisterRequest{AgentId: "drone-1"})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	second, err := r.Register(context.Background(), &agentv1.RegisterRequest{AgentId: "drone-1"})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if first.GetSessionId() == second.GetSessionId() {
		t.Fatalf("expected reconnect to generate a new session_id")
	}

	// Allow some refreshes to happen; ensure last observed session_id matches the latest.
	time.Sleep(120 * time.Millisecond)
	if got := store.lastSessionID(); got != second.GetSessionId() {
		t.Fatalf("expected routing mapping to reflect latest session_id; got %q want %q", got, second.GetSessionId())
	}
}
