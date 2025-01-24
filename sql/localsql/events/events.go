package events

import (
	"bytes"
	"fmt"
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/builder"
)

func InsertEvent(
	db sql.Executor, id types.NodeID, timestamp time.Time, kind int32, eventBytes []byte,
) error {
	enc := func(stmt *sql.Statement) {
		stmt.BindBytes(1, id.Bytes())
		stmt.BindInt64(2, timestamp.UnixMicro())
		stmt.BindInt64(3, int64(kind))
		stmt.BindBytes(4, eventBytes)
	}
	if _, err := db.Exec(
		`INSERT into events (id, timestamp, kind, event) values (?1, ?2, ?3, ?4);`, enc, nil); err != nil {
		return fmt.Errorf("inserting event for %s: %w", id.ShortString(), err)
	}
	return nil
}

func IterateEventsForID(
	db sql.Executor,
	id types.NodeID,
	fn func(
		timestamp time.Time,
		eventBytes []byte,
	) bool,
) error {
	var stateBuf bytes.Buffer
	_, err := db.Exec(
		`SELECT timestamp, event FROM events WHERE id = ?1 ORDER BY timestamp ASC`,
		func(s *sql.Statement) {
			s.BindBytes(1, id.Bytes())
		},
		func(stmt *sql.Statement) bool {
			timestamp := time.UnixMicro(stmt.ColumnInt64(0))
			stateBuf.Reset()
			stateBuf.ReadFrom(stmt.ColumnReader(1))
			return fn(timestamp, stateBuf.Bytes())
		},
	)
	if err != nil {
		return fmt.Errorf("iterating events for ID %s: %w", id.ShortString(), err)
	}
	return nil
}

func IterateAllEvents(
	db sql.Executor,
	operations builder.Operations,
	fn func(
		id types.NodeID,
		timestamp time.Time,
		eventBytes []byte,
	) bool,
) error {
	var stateBuf bytes.Buffer
	_, err := db.Exec(`SELECT id, timestamp, event FROM events`+builder.FilterFrom(operations),
		builder.BindingsFrom(operations),
		func(stmt *sql.Statement) bool {
			var id types.NodeID
			stmt.ColumnBytes(0, id[:])
			timestamp := time.UnixMicro(stmt.ColumnInt64(1))
			stateBuf.Reset()
			stateBuf.ReadFrom(stmt.ColumnReader(2))
			return fn(id, timestamp, stateBuf.Bytes())
		},
	)
	if err != nil {
		return fmt.Errorf("iterate events: %w", err)
	}
	return nil
}

func DeleteEventsOlderThan(db sql.Executor, timestamp time.Time) error {
	_, err := db.Exec(
		`DELETE FROM events WHERE timestamp < ?1;`,
		func(stmt *sql.Statement) {
			stmt.BindInt64(1, timestamp.UnixMicro())
		}, nil)
	if err != nil {
		return fmt.Errorf("deleting events older than %s: %w", timestamp, err)
	}
	return nil
}
