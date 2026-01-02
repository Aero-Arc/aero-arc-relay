package relay

import (
	"context"
	"sync"
	"testing"
	"time"

	agentv1 "github.com/aero-arc/aero-arc-protos/gen/go/aeroarc/agent/v1"
	"google.golang.org/grpc/metadata"
)

type fakeRoutingStore struct {
	mu          sync.Mutex
	upserts     int
	deletes     int
	lastDroneID string
	lastSession string
	ch          chan struct{}
}

func (f *fakeRoutingStore) UpsertDroneRouting(ctx context.Context, droneID, relayID, sessionID string, ttl time.Duration) error {
	f.mu.Lock()
	f.upserts++
	f.lastDroneID = droneID
	f.lastSession = sessionID
	f.mu.Unlock()
	select {
	case f.ch <- struct{}{}:
	default:
	}
	return nil
}

func (f *fakeRoutingStore) DeleteDroneRouting(ctx context.Context, droneID string) error {
	f.mu.Lock()
	f.deletes++
	f.lastDroneID = droneID
	f.mu.Unlock()
	select {
	case f.ch <- struct{}{}:
	default:
	}
	return nil
}

func TestRedisRouting_RegisterPublishesAndRefreshes(t *testing.T) {
	store := &fakeRoutingStore{ch: make(chan struct{}, 10)}
	r := &Relay{
		grpcSessions:                make(map[string]*DroneSession),
		redisRoutingStore:           store,
		redisRoutingTTL:             40 * time.Millisecond,
		redisRoutingCancelByDroneID: make(map[string]context.CancelFunc),
	}

	resp, err := r.Register(context.Background(), &agentv1.RegisterRequest{AgentId: "agent-1"})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if resp.SessionId == "" {
		t.Fatalf("expected session id")
	}

	// Expect immediate publish.
	select {
	case <-store.ch:
	case <-time.After(250 * time.Millisecond):
		t.Fatalf("expected redis upsert")
	}

	// Expect at least one refresh tick.
	select {
	case <-store.ch:
	case <-time.After(300 * time.Millisecond):
		t.Fatalf("expected redis refresh upsert")
	}

	store.mu.Lock()
	lastSession := store.lastSession
	store.mu.Unlock()
	if lastSession != resp.SessionId {
		t.Fatalf("expected latest session in upsert; got %q want %q", lastSession, resp.SessionId)
	}
}

func TestRedisRouting_TelemetryStreamDisconnectDeletesMapping(t *testing.T) {
	store := &fakeRoutingStore{ch: make(chan struct{}, 10)}
	r := &Relay{
		grpcSessions:                make(map[string]*DroneSession),
		redisRoutingStore:           store,
		redisRoutingTTL:             50 * time.Millisecond,
		redisRoutingCancelByDroneID: make(map[string]context.CancelFunc),
	}

	_, err := r.Register(context.Background(), &agentv1.RegisterRequest{AgentId: "agent-2"})
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("aero-arc-agent-id", "agent-2"))
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stream := &mockTelemetryStream{
		ctx:         ctx,
		recvChan:    make(chan *agentv1.TelemetryFrame, 1),
		sentAckChan: make(chan *agentv1.TelemetryAck, 1),
		errChan:     make(chan error, 1),
	}

	// Close stream to trigger io.EOF -> deferred delete runs.
	close(stream.recvChan)
	_ = r.TelemetryStream(stream)

	// Expect a delete call (best-effort).
	deadline := time.After(250 * time.Millisecond)
	for {
		store.mu.Lock()
		deletes := store.deletes
		store.mu.Unlock()
		if deletes > 0 {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("expected redis delete on disconnect")
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}
}
