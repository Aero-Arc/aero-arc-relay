package relay

import (
	"context"

	pb "github.com/aero-arc/aero-arc-protos/gen/go/aeroarc/relay/v1"
)

func (s *Relay) ListActiveDrones(ctx context.Context, req *pb.ListActiveDronesRequest) (*pb.ListActiveDronesResponse, error) {
	// Example of how you will eventually map it:
	// sessions := s.store.GetActiveDrones()
	// response := make([]*pb.DroneStatus, len(sessions))
	// ... mapping logic ...
	return &pb.ListActiveDronesResponse{}, nil
}

func (s *Relay) GetDroneStatus(ctx context.Context, req *pb.GetDroneStatusRequest) (*pb.GetDroneStatusResponse, error) {
	return &pb.GetDroneStatusResponse{}, nil
}
