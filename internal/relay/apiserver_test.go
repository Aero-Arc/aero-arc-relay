package relay

import (
	"context"
	"testing"

	relayv1 "github.com/aero-arc/aero-arc-protos/gen/go/aeroarc/relay/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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

func TestRelay_GetDroneStatus_ReturnsSessionWhenConnected(t *testing.T) {
	ds := &relayv1.DroneStatus{DroneId: "drone-123", SessionId: "sess-123"}
	r := &Relay{
		grpcSessions: map[string]*relayv1.DroneStatus{
			"drone-123": ds,
		},
	}

	resp, err := r.GetDroneStatus(context.Background(), &relayv1.GetDroneStatusRequest{DroneId: "drone-123"})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if resp == nil || resp.Drone == nil {
		t.Fatalf("expected non-nil response with drone")
	}
	if resp.Drone.GetDroneId() != "drone-123" {
		t.Fatalf("expected drone_id 'drone-123', got %q", resp.Drone.GetDroneId())
	}
	if resp.Drone.GetSessionId() != "sess-123" {
		t.Fatalf("expected session_id 'sess-123', got %q", resp.Drone.GetSessionId())
	}

	// No side effects: session state should be unchanged.
	if r.grpcSessions["drone-123"] != ds {
		t.Fatalf("expected grpcSessions entry pointer to remain unchanged")
	}
}

func TestRelay_GetDroneStatus_ReturnsNotFoundWhenAbsent(t *testing.T) {
	r := &Relay{
		grpcSessions: map[string]*relayv1.DroneStatus{},
	}

	_, err := r.GetDroneStatus(context.Background(), &relayv1.GetDroneStatusRequest{DroneId: "missing"})
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if status.Code(err) != codes.NotFound {
		t.Fatalf("expected NOT_FOUND, got %s (%v)", status.Code(err), err)
	}
}

func TestRelay_GetDroneStatus_ReturnsInvalidArgumentWhenDroneIDMissing(t *testing.T) {
	r := &Relay{
		grpcSessions: map[string]*relayv1.DroneStatus{},
	}

	_, err := r.GetDroneStatus(context.Background(), &relayv1.GetDroneStatusRequest{})
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("expected INVALID_ARGUMENT, got %s (%v)", status.Code(err), err)
	}
}


