package events

import (
	"bytes"
	"fmt"
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
)

func InsertEvent(db sql.Executor, id types.NodeID, timestamp time.Time, state []byte) error {
	enc := func(stmt *sql.Statement) {
		stmt.BindBytes(1, id.Bytes())
		stmt.BindInt64(2, timestamp.UnixMicro())
		stmt.BindBytes(3, state)
	}
	if _, err := db.Exec(`INSERT into events (id, timestamp, state) values (?1, ?2, ?3);`, enc, nil); err != nil {
		return fmt.Errorf("inserting event for %s: %w", id.ShortString(), err)
	}
	return nil
}

func IterateAllEvents(
	db sql.Executor,
	fn func(
		id types.NodeID,
		timestamp time.Time,
		state []byte,
	) bool,
) error {
	var stateBuf bytes.Buffer
	_, err := db.Exec(
		`SELECT id, timestamp, state FROM states`,
		nil,
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
