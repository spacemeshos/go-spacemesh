package malsync

import (
	"fmt"
	"time"

	"github.com/spacemeshos/go-spacemesh/sql"
)

func GetSyncState(db sql.Executor) (time.Time, error) {
	var timestamp time.Time
	rows, err := db.Exec("select timestamp from malfeasance_sync_state",
		nil, func(stmt *sql.Statement) bool {
			v := stmt.ColumnInt64(0)
			if v > 0 {
				timestamp = time.Unix(v, 0)
			}
			return true
		})
	if err != nil {
		return time.Time{}, fmt.Errorf("error getting malfeasance sync state: %w", err)
	} else if rows != 1 {
		return time.Time{}, fmt.Errorf("expected malfeasance_sync_state to have 1 row but got %d rows", rows)
	}
	return timestamp, nil
}

func updateSyncState(db sql.Executor, ts int64) error {
	_, err := db.Exec("update malfeasance_sync_state set timestamp = ?1",
		func(stmt *sql.Statement) {
			stmt.BindInt64(1, ts)
		}, nil)
	if err != nil {
		return fmt.Errorf("error updating malfeasance sync state: %w", err)
	}
	return nil
}

func UpdateSyncState(db sql.Executor, timestamp time.Time) error {
	return updateSyncState(db, timestamp.Unix())
}

func Clear(db sql.Executor) error {
	return updateSyncState(db, 0)
}
