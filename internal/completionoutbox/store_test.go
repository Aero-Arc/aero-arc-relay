// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. See https://mozilla.org/MPL/2.0/.
package completionoutbox

import (
	"context"
	pb "github.com/aero-arc/aero-arc-protos/gen/go/aeroarc/agent/v1"
	"google.golang.org/protobuf/proto"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCompletionDeliverySurvivesRestartAndRetainsIdentity(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "completion.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now().Add(-time.Hour).UnixNano()
	e := &pb.FlightCompletionEvidence{EventId: "event", AgentId: "agent", Context: &pb.OperationContext{AircraftId: "aircraft", FlightId: "flight", IntentId: "intent", IntentVersion: 1}, MissionId: "mission", MissionDigest: strings.Repeat("a", 64), StartCommandId: "start", Outcome: "mission_completed", AirborneAtUnixNs: at, TerminalAtUnixNs: at + int64(time.Minute), LandedAtUnixNs: at + int64(2*time.Minute), DisarmedAtUnixNs: at + int64(2*time.Minute), ObservationEpoch: "epoch"}
	receipt, err := s.Admit(ctx, e)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	}()
	pending, err := s.Pending(ctx, 10)
	if err != nil || len(pending) != 1 || !proto.Equal(pending[0], e) {
		t.Fatalf("restart pending=%v err=%v", pending, err)
	}
	replay, err := s.Admit(ctx, e)
	if err != nil || !proto.Equal(receipt, replay) {
		t.Fatalf("duplicate=%v err=%v", replay, err)
	}
	bad := proto.Clone(receipt).(*pb.FlightCompletionReceipt)
	bad.PayloadSha256 = strings.Repeat("b", 64)
	if s.Acknowledge(ctx, bad) == nil {
		t.Fatal("mismatched receipt retired evidence")
	}
	for i := 0; i < 2; i++ {
		if err = s.Acknowledge(ctx, receipt); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = s.Admit(ctx, e); err != nil {
		t.Fatal(err)
	}
	pending, err = s.Pending(ctx, 10)
	if err != nil || len(pending) != 0 {
		t.Fatalf("acknowledged duplicate requeued: %v %v", pending, err)
	}
	changed := proto.Clone(e).(*pb.FlightCompletionEvidence)
	changed.Outcome = "ended_early"
	if _, err = s.Admit(ctx, changed); err == nil {
		t.Fatal("changed delivered event accepted")
	}
}
