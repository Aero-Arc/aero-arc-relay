package relay

import (
	"context"

	pb "github.com/aero-arc/aero-arc-protos/gen/go/aeroarc/relay/v1"
)

// RelayControl is a read-only view of relay session state.
//
// IMPORTANT CONTRACT:
//   - RelayControl RPCs MUST NOT mutate in-memory session state.
//   - AgentGateway is the sole writer for session lifecycle and telemetry-driven updates.
//   - Do not introduce control-plane commands here in v0.1; add a separate service if/when
//     mutating operations are needed.
//
// This guard exists to prevent accidental command creep and keep ownership rules crisp.
func (s *Relay) ListActiveDrones(ctx context.Context, req *pb.ListActiveDronesRequest) (*pb.ListActiveDronesResponse, error) {
	// Example of how you will eventually map it:
	// sessions := s.store.GetActiveDrones()
	// response := make([]*pb.DroneStatus, len(sessions))
	// ... mapping logic ...
	return &pb.ListActiveDronesResponse{}, nil
}

// GetDroneStatus is a read-only RPC; see the RelayControl contract above.
func (s *Relay) GetDroneStatus(ctx context.Context, req *pb.GetDroneStatusRequest) (*pb.GetDroneStatusResponse, error) {
	return &pb.GetDroneStatusResponse{}, nil
}
