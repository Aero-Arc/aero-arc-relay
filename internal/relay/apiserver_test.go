package relay

import (
	"context"
	"testing"

	relayv1 "github.com/aero-arc/aero-arc-protos/gen/go/aeroarc/relay/v1"
)

func TestRelay_ListActiveDrones_Empty(t *testing.T) {
	r := &Relay{
		grpcSessions: make(map[string]*relayv1.DroneStatus),
	}

	resp, err := r.ListActiveDrones(context.Background(), &relayv1.ListActiveDronesRequest{})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if resp == nil {
		t.Fatalf("expected non-nil response")
	}
	if resp.Drones == nil {
		t.Fatalf("expected non-nil drones slice (empty), got nil")
	}
	if got := len(resp.Drones); got != 0 {
		t.Fatalf("expected 0 drones, got %d", got)
	}
}

func TestRelay_ListActiveDrones_ReturnsSortedActiveSessions(t *testing.T) {
	droneA := &relayv1.DroneStatus{DroneId: "drone-a", SessionId: "s-a"}
	droneB := &relayv1.DroneStatus{DroneId: "drone-b", SessionId: "s-b"}

	r := &Relay{
		grpcSessions: map[string]*relayv1.DroneStatus{
			"drone-b": droneB,
			"drone-a": droneA,
			// nil entries should be ignored
			"drone-nil": nil,
		},
	}

	resp, err := r.ListActiveDrones(context.Background(), &relayv1.ListActiveDronesRequest{})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if resp == nil {
		t.Fatalf("expected non-nil response")
	}

	if got := len(resp.Drones); got != 2 {
		t.Fatalf("expected 2 drones, got %d", got)
	}

	if resp.Drones[0].GetDroneId() != "drone-a" || resp.Drones[1].GetDroneId() != "drone-b" {
		t.Fatalf("expected sorted order [drone-a, drone-b], got [%s, %s]",
			resp.Drones[0].GetDroneId(), resp.Drones[1].GetDroneId())
	}
}


