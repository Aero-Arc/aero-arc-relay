// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. See https://mozilla.org/MPL/2.0/.
package relay

import (
	"context"
	pb "github.com/aero-arc/aero-arc-protos/gen/go/aeroarc/agent/v1"
	rpc "github.com/aero-arc/aero-arc-protos/gen/go/aeroarc/relay/v1"
	"github.com/makinje/aero-arc-relay/internal/completionoutbox"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"path/filepath"
	"testing"
	"time"
)

func TestCompletionAcknowledgementSeparatesReceiptAndStorageErrors(t *testing.T) {
	store, err := completionoutbox.Open(filepath.Join(t.TempDir(), "outbox.db"))
	if err != nil {
		t.Fatal(err)
	}
	r := &Relay{completionOutbox: store, controlAuthorizer: func(context.Context) error { return nil }}
	req := &rpc.AckFlightCompletionsRequest{Receipts: []*pb.FlightCompletionReceipt{{EventId: "absent", PayloadSha256: "wrong"}}}
	if _, err = r.AckFlightCompletions(context.Background(), req); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("invalid receipt: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = r.AckFlightCompletions(ctx, req); status.Code(err) != codes.Canceled {
		t.Fatalf("canceled write: %v", err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = r.AckFlightCompletions(context.Background(), req); status.Code(err) != codes.Unavailable {
		t.Fatalf("storage failure reported permanent: %v", err)
	}
}

func TestCompletionListPreservesContextStatus(t *testing.T) {
	store, err := completionoutbox.Open(filepath.Join(t.TempDir(), "outbox.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	r := &Relay{completionOutbox: store, controlAuthorizer: func(context.Context) error { return nil }}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	expired, stop := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer stop()
	for _, tc := range []struct {
		ctx  context.Context
		code codes.Code
	}{{canceled, codes.Canceled}, {expired, codes.DeadlineExceeded}} {
		if _, err := r.ListFlightCompletions(tc.ctx, &rpc.ListFlightCompletionsRequest{}); status.Code(err) != tc.code {
			t.Fatalf("got %v want %v", err, tc.code)
		}
	}
}
