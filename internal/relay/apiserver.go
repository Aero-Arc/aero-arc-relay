package relay

import (
	"context"
	"sort"

	relayv1 "github.com/aero-arc/aero-arc-protos/gen/go/aeroarc/relay/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ListActiveDrones implements aeroarc.relay.v1.RelayControlServer.
// It uses grpcSessions as the source of truth for live drone sessions.
func (s *Relay) ListActiveDrones(ctx context.Context, req *relayv1.ListActiveDronesRequest) (*relayv1.ListActiveDronesResponse, error) {
	s.grpcSessionsMu.RLock()
	defer s.grpcSessionsMu.RUnlock()

	drones := make([]*relayv1.DroneStatus, 0, len(s.grpcSessions))
	for _, ds := range s.grpcSessions {
		if ds == nil {
			continue
		}
		drones = append(drones, ds)
	}

	// Stable output ordering helps tests and makes UI diffs deterministic.
	sort.Slice(drones, func(i, j int) bool {
		return drones[i].GetDroneId() < drones[j].GetDroneId()
	})

	return &relayv1.ListActiveDronesResponse{Drones: drones}, nil
}

// GetDroneStatus implements aeroarc.relay.v1.RelayControlServer.
// It uses grpcSessions as the source of truth for a drone's live session.
func (s *Relay) GetDroneStatus(ctx context.Context, req *relayv1.GetDroneStatusRequest) (*relayv1.GetDroneStatusResponse, error) {
	droneID := req.GetDroneId()
	if droneID == "" {
		return nil, status.Error(codes.InvalidArgument, "drone_id is required")
	}

	s.grpcSessionsMu.RLock()
	ds := s.grpcSessions[droneID]
	s.grpcSessionsMu.RUnlock()

	if ds == nil {
		return nil, status.Error(codes.NotFound, "drone not found")
	}

	return &relayv1.GetDroneStatusResponse{Drone: ds}, nil
}


