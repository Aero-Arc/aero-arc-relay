/*
Copyright 2025 The Aero Arc Relay Authors.

Licensed under the Mozilla Public License, Version 2.0 (the "License");
You may obtain a copy of the License at http://mozilla.org.

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
*/

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
