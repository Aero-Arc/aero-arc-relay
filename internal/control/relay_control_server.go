package control

import (
	"context"
	"time"

	"github.com/makinje/aero-arc-relay/internal/config"
	"github.com/makinje/aero-arc-relay/internal/relay"
	"github.com/makinje/aero-arc-relay/pkg/controlpb"
)

// RelayControlServer implements the read-only control-plane gRPC APIs.
type RelayControlServer struct {
	startTime time.Time
	cfg       *config.Config
	relay     *relay.Relay
}

// NewRelayControlServer constructs a new RelayControlServer.
// - cfg:   loaded relay configuration (used to expose endpoint metadata).
// - r:     relay instance, reserved for richer status in the future.
// - start: relay process start time for computing uptime.
func NewRelayControlServer(cfg *config.Config, r *relay.Relay, start time.Time) *RelayControlServer {
	return &RelayControlServer{
		startTime: start,
		cfg:       cfg,
		relay:     r,
	}
}

// GetStatus returns a minimal view of the relay status.
// This is intentionally read-only and side-effect free.
func (s *RelayControlServer) GetStatus(ctx context.Context, _ *controlpb.GetStatusRequest) (*controlpb.GetStatusResponse, error) {
	uptimeSeconds := int64(time.Since(s.startTime).Seconds())

	return &controlpb.GetStatusResponse{
		Version:       "v0.1.0", // TODO: wire real version information at build time.
		State:         "RUNNING",
		UptimeSeconds: uptimeSeconds,
	}, nil
}

// ListEndpoints exposes the configured MAVLink endpoints.
// It reflects configuration only and does not mutate any state.
func (s *RelayControlServer) ListEndpoints(ctx context.Context, _ *controlpb.ListEndpointsRequest) (*controlpb.ListEndpointsResponse, error) {
	endpoints := make([]*controlpb.EndpointInfo, 0, len(s.cfg.MAVLink.Endpoints))

	for _, ep := range s.cfg.MAVLink.Endpoints {
		endpoints = append(endpoints, &controlpb.EndpointInfo{
			Name:     ep.Name,
			DroneID:  ep.DroneID,
			Protocol: ep.ProtocolName,
			Port:     int32(ep.Port),
		})
	}

	return &controlpb.ListEndpointsResponse{
		Endpoints: endpoints,
	}, nil
}


