package malsync

import (
	"fmt"
	"time"

	"github.com/spacemeshos/go-spacemesh/sql"
)

func LegacySyncState(db sql.Executor) (time.Time, error) {
	return getSyncState(db, 1)
}

func SyncState(db sql.Executor) (time.Time, error) {
	return getSyncState(db, 2)
}

func getSyncState(db sql.Executor, version int64) (time.Time, error) {
	var timestamp time.Time
	rows, err := db.Exec("select timestamp from malfeasance_sync_state where id = ?1",
		func(s *sql.Statement) {
			s.BindInt64(1, version)
		},
		func(stmt *sql.Statement) bool {
			v := stmt.ColumnInt64(0)
			if v > 0 {
				timestamp = time.Unix(v, 0)
			}
			return true
		})
	switch {
	case err != nil:
		return time.Time{}, fmt.Errorf("error getting malfeasance sync state: %w", err)
	case rows <= 1:
		return timestamp, nil
	default:
		return time.Time{}, fmt.Errorf("expected malfeasance_sync_state to have 1 row but got %d rows", rows)
	}
}

func updateSyncState(db sql.Executor, version, ts int64) error {
	if _, err := db.Exec(`
		insert into malfeasance_sync_state (id, timestamp) values(?1, ?2)
        on conflict (id) do update set timestamp = ?2
	`, func(stmt *sql.Statement) {
		stmt.BindInt64(1, version)
		stmt.BindInt64(2, ts)
	}, nil,
	); err != nil {
		return fmt.Errorf("error initializing malfeasance sync state: %w", err)
	}
	return nil
}

func UpdateLegacySyncState(db sql.Executor, timestamp time.Time) error {
	return updateSyncState(db, 1, timestamp.Unix())
}

func UpdateSyncState(db sql.Executor, timestamp time.Time) error {
	return updateSyncState(db, 2, timestamp.Unix())
}

func Clear(db sql.Executor) error {
	if err := updateSyncState(db, 1, 0); err != nil {
		return err
	}
	return updateSyncState(db, 2, 0)
}
