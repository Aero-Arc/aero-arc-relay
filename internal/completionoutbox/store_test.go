// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. See https://mozilla.org/MPL/2.0/.
package completionoutbox

import (
	"context"
	"fmt"
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

func TestRejectsNonDurableSQLitePaths(t *testing.T) {
	for _, path := range []string{"", ":memory:", "file::memory:?cache=shared", "file:completion?mode=memory&cache=shared", "file:/tmp/completion?mode=memory"} {
		t.Run(path, func(t *testing.T) {
			s, err := Open(path)
			if err == nil {
				_ = s.Close()
				t.Fatal("nonpersistent path accepted")
			}
		})
	}
}

func TestPendingRotatesPastUnadmittedPageAcrossRestart(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "rotation.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 101; i++ {
		raw, err := proto.Marshal(&pb.FlightCompletionEvidence{EventId: fmt.Sprint(i)})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.db.Exec(`INSERT INTO flight_completions(event_id,digest,payload) VALUES(?,?,?)`, fmt.Sprint(i), "digest", raw); err != nil {
			t.Fatal(err)
		}
	}
	page, err := s.Pending(ctx, 100)
	if err != nil || len(page) != 100 {
		t.Fatalf("first page=%d err=%v", len(page), err)
	}
	// Simulate API rejecting all 100: no acknowledgement is issued.
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	page, err = s.Pending(ctx, 100)
	if err != nil || len(page) != 100 || page[0].EventId != "100" {
		t.Fatalf("later valid event starved: %v %v", page, err)
	}
	var pending int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM flight_completions WHERE delivered=0`).Scan(&pending); err != nil || pending != 101 {
		t.Fatalf("evidence lost: %d %v", pending, err)
	}
}
