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
	"strings"

	"github.com/aero-arc/aero-arc-protos/flightcompletion"
	pb "github.com/aero-arc/aero-arc-protos/gen/go/aeroarc/agent/v1"
	"google.golang.org/protobuf/proto"
	_ "modernc.org/sqlite"
)

// ErrConflict indicates changed content under an immutable event identity.
var ErrConflict = errors.New("immutable completion event conflict")

// ErrInvalid identifies malformed completion evidence.
var ErrInvalid = errors.New("invalid completion evidence")

// ErrReceipt identifies a missing or mismatched completion acknowledgement.
var ErrReceipt = errors.New("invalid completion receipt")

// Store is a restart-durable, at-least-once completion notification outbox.
// Acknowledged rows remain as deduplication tombstones; retention is explicit.
type Store struct{ db *sql.DB }

// Open initializes a single-connection SQLite FULL/WAL store on persistent disk.
//
// Parameters: path names a durable SQLite file; empty, in-memory, and SQLite URI paths fail.
// Returns: an initialized store, or a path, filesystem, connection, or schema
// error. Existing pending events and acknowledgement tombstones are preserved.
func Open(path string) (*Store, error) {
	if path == "" || path == ":memory:" || strings.HasPrefix(path, "file:") {
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
	for _, q := range []string{`PRAGMA journal_mode=WAL`, `PRAGMA synchronous=FULL`, `PRAGMA busy_timeout=5000`, `CREATE TABLE IF NOT EXISTS flight_completions(event_id TEXT PRIMARY KEY,digest TEXT NOT NULL,payload BLOB NOT NULL,delivered INTEGER NOT NULL DEFAULT 0)`, `CREATE TABLE IF NOT EXISTS completion_delivery_rotation(event_id TEXT PRIMARY KEY, turn INTEGER NOT NULL)`} {
		if _, err = db.Exec(q); err != nil {
			_ = db.Close()
			return nil, err
		}
	}
	var mode, filename string
	var sequence int
	var name string
	if err = db.QueryRow(`PRAGMA journal_mode`).Scan(&mode); err == nil {
		err = db.QueryRow(`PRAGMA database_list`).Scan(&sequence, &name, &filename)
	}
	if err != nil || mode != "wal" || filename == "" {
		_ = db.Close()
		return nil, fmt.Errorf("completion outbox requires on-disk WAL storage: mode=%q file=%q: %v", mode, filename, err)
	}
	return &Store{db: db}, nil
}

// Close releases the database connection without acknowledging pending evidence.
// Stop delivery workers first. Closing prevents new queries; database/sql drains
// queries already processing. Repeated and concurrent calls are safe.
//
// Parameters: none.
// Returns: the database close error, if any. Pending events and deduplication
// tombstones remain on disk for Open; subsequent store operations fail.
func (s *Store) Close() error { return s.db.Close() }

// Admit commits immutable evidence before producing its exact delivery receipt.
// Changed content under an existing event ID is rejected even after delivery.
//
// Parameters: ctx bounds SQLite admission; e supplies immutable, validated flight
// binding and completion milestones from an authenticated Agent.
// Returns: the exact event/digest receipt after commit, ErrInvalid for malformed
// evidence, ErrConflict for changed identity content, or a storage error. Exact
// replay returns the original receipt without reviving an acknowledged event.
func (s *Store) Admit(ctx context.Context, e *pb.FlightCompletionEvidence) (*pb.FlightCompletionReceipt, error) {
	raw, digest, err := flightcompletion.Encode(e)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	if _, err = s.db.ExecContext(ctx, `INSERT INTO flight_completions(event_id,digest,payload) VALUES(?,?,?) ON CONFLICT(event_id) DO NOTHING`, e.EventId, digest, raw); err != nil {
		return nil, err
	}
	var saved string
	if err = s.db.QueryRowContext(ctx, `SELECT digest FROM flight_completions WHERE event_id=?`, e.EventId).Scan(&saved); err != nil {
		return nil, err
	}
	if saved != digest {
		return nil, ErrConflict
	}
	return &pb.FlightCompletionReceipt{EventId: e.EventId, PayloadSha256: digest}, nil
}

// Pending returns a bounded page without removing delivery obligations.
//
// Parameters: ctx bounds reads; limit must be between 1 and 200 inclusive.
// Returns: pending immutable events in durable least-recently-offered order, or a limit, storage, or
// decoding error. Offering a page only rotates its scheduling priority; failed
// admissions remain pending and will be offered again. Callers must explicitly
// acknowledge successful durable delivery.
func (s *Store) Pending(ctx context.Context, limit int) ([]*pb.FlightCompletionEvidence, error) {
	if limit < 1 || limit > 200 {
		return nil, fmt.Errorf("limit must be 1..200")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	rows, err := tx.QueryContext(ctx, `SELECT e.payload FROM flight_completions e LEFT JOIN completion_delivery_rotation r ON r.event_id=e.event_id WHERE e.delivered=0 ORDER BY COALESCE(r.turn,0),e.rowid LIMIT ?`, limit)
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
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if err = rows.Close(); err != nil {
		return nil, err
	}
	var turn int64
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(turn),0)+1 FROM completion_delivery_rotation`).Scan(&turn); err != nil {
		return nil, err
	}
	for _, e := range result {
		if _, err = tx.ExecContext(ctx, `INSERT INTO completion_delivery_rotation(event_id,turn) VALUES(?,?) ON CONFLICT(event_id) DO UPDATE SET turn=excluded.turn`, e.EventId, turn); err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return result, nil
}

// Acknowledge removes the pending obligation only for an exact admitted digest.
//
// Parameters: ctx bounds persistence; r identifies an admitted event and its
// exact encoded digest after the consumer has committed durable admission.
// Returns: nil after acknowledgement or an exact duplicate; a nil/mismatched
// receipt (ErrReceipt) or storage error leaves the obligation pending. Acknowledged
// payload bytes are released for SQLite reuse; identity/digest tombstones remain.
func (s *Store) Acknowledge(ctx context.Context, r *pb.FlightCompletionReceipt) error {
	if r == nil {
		return fmt.Errorf("%w: receipt required", ErrReceipt)
	}
	result, err := s.db.ExecContext(ctx, `UPDATE flight_completions SET delivered=1,payload=X'' WHERE event_id=? AND digest=?`, r.EventId, r.PayloadSha256)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("%w: completion receipt does not match stored event", ErrReceipt)
	}
	return nil
}
