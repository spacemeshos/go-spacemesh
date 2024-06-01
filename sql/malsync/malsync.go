package malsync

import (
	"fmt"
	"time"

	"github.com/spacemeshos/go-spacemesh/sql"
)

func GetSyncState(db sql.Executor) (time.Time, error) {
	timestamp, _, err := getSyncState(db)
	return timestamp, err
}

func getSyncState(db sql.Executor) (time.Time, bool, error) {
	var timestamp time.Time
	rows, err := db.Exec("select timestamp from malfeasance_sync_state",
		nil, func(stmt *sql.Statement) bool {
			v := stmt.ColumnInt64(0)
			if v > 0 {
				timestamp = time.Unix(v, 0)
			}
			return true
		})
	switch {
	case err != nil:
		return time.Time{}, false, fmt.Errorf("error getting malfeasance sync state: %w", err)
	case rows == 0:
		return timestamp, false, nil
	case rows == 1:
		return timestamp, true, nil
	default:
		return time.Time{}, false, fmt.Errorf("expected malfeasance_sync_state to have 1 row but got %d rows", rows)
	}
}

func updateSyncState(db sql.Executor, ts int64) error {
	_, haveTS, err := getSyncState(db)
	if err != nil {
		return err
	}
	enc := func(stmt *sql.Statement) {
		stmt.BindInt64(1, ts)
	}
	if haveTS {
		_, err := db.Exec("update malfeasance_sync_state set timestamp = ?1", enc, nil)
		if err != nil {
			return fmt.Errorf("error updating malfeasance sync state: %w", err)
		}
	} else {
		_, err = db.Exec("insert into malfeasance_sync_state (timestamp) values(?1)", enc, nil)
		if err != nil {
			return fmt.Errorf("error initializing malfeasance sync state: %w", err)
		}
	}
	return nil
}

func UpdateSyncState(db sql.Executor, timestamp time.Time) error {
	return updateSyncState(db, timestamp.Unix())
}

func Clear(db sql.Executor) error {
	return updateSyncState(db, 0)
}
