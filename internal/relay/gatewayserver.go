package relay

import (
	"context"
	"io"
	"log/slog"
	"time"

	agentv1 "github.com/aero-arc/aero-arc-protos/gen/go/aeroarc/agent/v1"
	relayv1 "github.com/aero-arc/aero-arc-protos/gen/go/aeroarc/relay/v1"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const defaultMaxInflight = int64(128)

func (r *Relay) upsertRedisRouting(ctx context.Context, ds *relayv1.DroneStatus) {
	if r.redisRoutingStore == nil || ds == nil {
		return
	}
	ttl := r.redisRoutingTTL
	if ttl <= 0 {
		ttl = 45 * time.Second
	}
	// Always put a time bound on the Redis write.
	opCtx, cancel := context.WithTimeout(ctx, 750*time.Millisecond)
	defer cancel()
	if err := r.redisRoutingStore.UpsertDroneRouting(opCtx, ds.GetDroneId(), ds.GetRelayId(), ds.GetSessionId(), ttl); err != nil {
		slog.LogAttrs(ctx, slog.LevelWarn, "Failed to upsert drone routing metadata to Redis", slog.String("error", err.Error()), slog.String("drone_id", ds.GetDroneId()))
	}
}

func (r *Relay) startRedisRoutingRefreshLoop(sessionID string, cancelCtx context.Context, ttl time.Duration) {
	// Initial write immediately.
	r.grpcSessionsMu.RLock()
	ds := r.sessionByID[sessionID]
	r.grpcSessionsMu.RUnlock()
	if ds != nil {
		r.upsertRedisRouting(cancelCtx, ds)
	}

	interval := ttl / 2
	if interval <= 0 {
		interval = ttl
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-cancelCtx.Done():
			return
		case <-ticker.C:
			r.grpcSessionsMu.RLock()
			ds := r.sessionByID[sessionID]
			r.grpcSessionsMu.RUnlock()
			if ds == nil {
				return
			}
			r.upsertRedisRouting(cancelCtx, ds)
		}
	}
}

// Register implements aeroarc.agent.v1.AgentGatewayServer.
//
// Behavior:
// - Creates a new server-issued session_id.
// - Creates/overwrites in-memory session state keyed by drone_id.
//
// NOTE: The current RegisterRequest proto does not include a drone_id.
// For now, we treat agent_id as the stable drone_id. When the contract
// includes drone identity, this mapping should be updated.
func (r *Relay) Register(ctx context.Context, req *agentv1.RegisterRequest) (*agentv1.RegisterResponse, error) {
	agentID := req.GetAgentId()
	if agentID == "" {
		return nil, status.Error(codes.InvalidArgument, "agent_id is required")
	}

	droneID := agentID
	sessionID := uuid.NewString()
	now := time.Now().UTC().UnixNano()

	ds := &relayv1.DroneStatus{
		DroneId:             droneID,
		SessionId:           sessionID,
		AgentId:             agentID,
		RelayId:             "relay", // TODO: wire a real relay_id identifier
		ConnectedAtUnixNs:   now,
		LastHeartbeatUnixNs: now,
	}

	var startRefreshLoop func()
	r.grpcSessionsMu.Lock()
	// If this drone is re-registering, stop refreshing the old session and drop it
	// so Redis doesn't flap between old/new session IDs.
	if existing := r.grpcSessions[droneID]; existing != nil && existing.GetSessionId() != "" && existing.GetSessionId() != sessionID {
		oldSessionID := existing.GetSessionId()
		if cancel := r.redisRoutingCancelBySessionID[oldSessionID]; cancel != nil {
			cancel()
			delete(r.redisRoutingCancelBySessionID, oldSessionID)
		}
		delete(r.sessionByID, oldSessionID)
	}

	r.grpcSessions[droneID] = ds
	r.sessionByID[sessionID] = ds

	// Start a best-effort TTL refresher for this session.
	if r.redisRoutingStore != nil {
		ttl := r.redisRoutingTTL
		if ttl <= 0 {
			ttl = 45 * time.Second
		}
		cancelCtx, cancel := context.WithCancel(context.Background())
		r.redisRoutingCancelBySessionID[sessionID] = cancel
		startRefreshLoop = func() { go r.startRedisRoutingRefreshLoop(sessionID, cancelCtx, ttl) }
	}
	r.grpcSessionsMu.Unlock()

	if startRefreshLoop != nil {
		startRefreshLoop()
	}

	// Ensure mapping exists promptly for callers even before the first refresh tick.
	r.upsertRedisRouting(ctx, ds)

	return &agentv1.RegisterResponse{
		AgentId:     agentID,
		SessionId:   sessionID,
		MaxInflight: defaultMaxInflight,
	}, nil
}

// TelemetryStream implements aeroarc.agent.v1.AgentGatewayServer.
//
// Behavior:
// - On telemetry activity, updates last_heartbeat for the session.
// - On disconnect/stream close, removes the session from in-memory state.
func (r *Relay) TelemetryStream(stream grpc.BidiStreamingServer[agentv1.TelemetryFrame, agentv1.TelemetryAck]) error {
	var sessionID string

	cleanup := func() {
		if sessionID == "" {
			return
		}
		r.removeSessionByID(sessionID)
	}
	defer cleanup()

	for {
		frame, err := stream.Recv()
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}

		if sessionID == "" {
			sessionID = frame.GetSessionId()
		}

		if frame.GetSessionId() == "" {
			// Can't associate this frame with a session.
			_ = stream.Send(&agentv1.TelemetryAck{Seq: frame.GetSeq(), Status: agentv1.TelemetryAck_STATUS_PERMANENT_ERROR, Error: "missing session_id"})
			continue
		}

		r.touchSessionByID(frame.GetSessionId())

		_ = stream.Send(&agentv1.TelemetryAck{Seq: frame.GetSeq(), Status: agentv1.TelemetryAck_STATUS_OK})
	}
}

func (r *Relay) touchSessionByID(sessionID string) {
	now := time.Now().UTC().UnixNano()

	r.grpcSessionsMu.Lock()
	defer r.grpcSessionsMu.Unlock()

	ds := r.sessionByID[sessionID]
	if ds == nil {
		return
	}
	ds.LastHeartbeatUnixNs = now
}

func (r *Relay) removeSessionByID(sessionID string) {
	r.grpcSessionsMu.Lock()
	defer r.grpcSessionsMu.Unlock()

	if cancel := r.redisRoutingCancelBySessionID[sessionID]; cancel != nil {
		cancel()
		delete(r.redisRoutingCancelBySessionID, sessionID)
	}

	ds := r.sessionByID[sessionID]
	if ds == nil {
		return
	}
	// Best-effort delete on clean disconnect; TTL remains the crash-safe fallback.
	if r.redisRoutingStore != nil {
		_ = r.redisRoutingStore.DeleteDroneRouting(context.Background(), ds.GetDroneId())
	}
	delete(r.sessionByID, sessionID)
	// grpcSessions is keyed by drone_id.
	delete(r.grpcSessions, ds.GetDroneId())
}
