// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. See https://mozilla.org/MPL/2.0/.
package completionoutbox

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/aero-arc/aero-arc-protos/flightcompletion"
	pb "github.com/aero-arc/aero-arc-protos/gen/go/aeroarc/agent/v1"
	"google.golang.org/protobuf/proto"
	_ "modernc.org/sqlite"
)

// Store is a restart-durable, at-least-once completion notification outbox.
// Acknowledged rows remain as deduplication tombstones; retention is explicit.
type Store struct{ db *sql.DB }

// Open initializes a single-connection SQLite FULL/WAL store on persistent disk.
func Open(path string) (*Store, error) {
	if path == "" || path == ":memory:" {
		return nil, fmt.Errorf("durable completion outbox path required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	for _, q := range []string{`PRAGMA journal_mode=WAL`, `PRAGMA synchronous=FULL`, `PRAGMA busy_timeout=5000`, `CREATE TABLE IF NOT EXISTS flight_completions(event_id TEXT PRIMARY KEY,digest TEXT NOT NULL,payload BLOB NOT NULL,delivered INTEGER NOT NULL DEFAULT 0)`} {
		if _, err = db.Exec(q); err != nil {
			_ = db.Close()
			return nil, err
		}
	}
	return &Store{db: db}, nil
}

// Close flushes and releases the database connection.
func (s *Store) Close() error { return s.db.Close() }

// Admit commits immutable evidence before producing its exact delivery receipt.
// Changed content under an existing event ID is rejected even after delivery.
func (s *Store) Admit(ctx context.Context, e *pb.FlightCompletionEvidence) (*pb.FlightCompletionReceipt, error) {
	raw, digest, err := flightcompletion.Encode(e)
	if err != nil {
		return nil, err
	}
	if _, err = s.db.ExecContext(ctx, `INSERT INTO flight_completions(event_id,digest,payload) VALUES(?,?,?) ON CONFLICT(event_id) DO NOTHING`, e.EventId, digest, raw); err != nil {
		return nil, err
	}
	var saved string
	if err = s.db.QueryRowContext(ctx, `SELECT digest FROM flight_completions WHERE event_id=?`, e.EventId).Scan(&saved); err != nil {
		return nil, err
	}
	if saved != digest {
		return nil, fmt.Errorf("immutable completion event conflict")
	}
	return &pb.FlightCompletionReceipt{EventId: e.EventId, PayloadSha256: digest}, nil
}

// Pending returns a bounded page without removing delivery obligations.
func (s *Store) Pending(ctx context.Context, limit int) ([]*pb.FlightCompletionEvidence, error) {
	if limit < 1 || limit > 200 {
		return nil, fmt.Errorf("limit must be 1..200")
	}
	rows, err := s.db.QueryContext(ctx, `SELECT payload FROM flight_completions WHERE delivered=0 ORDER BY rowid LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	result := make([]*pb.FlightCompletionEvidence, 0)
	for rows.Next() {
		var raw []byte
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		e := new(pb.FlightCompletionEvidence)
		if err = proto.Unmarshal(raw, e); err != nil {
			return nil, err
		}
		result = append(result, e)
	}
	return result, rows.Err()
}

// Acknowledge removes the pending obligation only for an exact admitted digest.
func (s *Store) Acknowledge(ctx context.Context, r *pb.FlightCompletionReceipt) error {
	if r == nil {
		return errors.New("receipt required")
	}
	result, err := s.db.ExecContext(ctx, `UPDATE flight_completions SET delivered=1 WHERE event_id=? AND digest=?`, r.EventId, r.PayloadSha256)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return errors.New("completion receipt does not match stored event")
	}
	return nil
}
