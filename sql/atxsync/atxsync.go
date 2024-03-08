package atxsync

import (
	"fmt"
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
)

func GetSyncState(db sql.Executor, epoch types.EpochID) (map[types.ATXID]int, error) {
	states := map[types.ATXID]int{}
	_, err := db.Exec("select id, requests from atx_sync_state where epoch = ?1",
		func(stmt *sql.Statement) {
			stmt.BindInt64(1, int64(epoch))
		}, func(stmt *sql.Statement) bool {
			var id types.ATXID
			stmt.ColumnBytes(0, id[:])
			states[id] = int(stmt.ColumnInt64(1))
			return true
		})
	if err != nil {
		return nil, fmt.Errorf("select synced atx ids for epoch failed %v: %w", epoch, err)
	}
	return states, nil
}

func SaveSyncState(db sql.Executor, epoch types.EpochID, states map[types.ATXID]int, max int) error {
	for id, requests := range states {
		var err error
		if requests == max {
			_, err = db.Exec(`delete from atx_sync_state where epoch = ?1 and id = ?2`, func(stmt *sql.Statement) {
				stmt.BindInt64(1, int64(epoch))
				stmt.BindBytes(2, id[:])
			}, nil)
		} else {
			_, err = db.Exec(`insert into atx_sync_state 
		(epoch, id, requests) values (?1, ?2, ?3)
		on conflict(epoch, id) do update set requests = ?3;`,
				func(stmt *sql.Statement) {
					stmt.BindInt64(1, int64(epoch))
					stmt.BindBytes(2, id[:])
					stmt.BindInt64(3, int64(requests))
				}, nil)
		}
		if err != nil {
			return fmt.Errorf("insert synced atx id %v/%v failed: %w", epoch, id.ShortString(), err)
		}
	}
	return nil
}

func SaveRequestTime(db sql.Executor, epoch types.EpochID, timestamp time.Time) error {
	_, err := db.Exec(`insert into atx_sync_requests 
	(epoch, timestamp) values (?1, ?2)
	on conflict(epoch) do update set timestamp = ?2;`,
		func(stmt *sql.Statement) {
			stmt.BindInt64(1, int64(epoch))
			stmt.BindInt64(2, timestamp.Unix())
		}, nil)
	if err != nil {
		return fmt.Errorf("insert request time for epoch %v failed: %w", epoch, err)
	}
	return nil
}

func GetRequestTime(db sql.Executor, epoch types.EpochID) (time.Time, error) {
	var timestamp time.Time
	rows, err := db.Exec("select timestamp from atx_sync_requests where epoch = ?1",
		func(stmt *sql.Statement) {
			stmt.BindInt64(1, int64(epoch))
		}, func(stmt *sql.Statement) bool {
			timestamp = time.Unix(stmt.ColumnInt64(0), 0)
			return true
		})
	if err != nil {
		return time.Time{}, fmt.Errorf("select request time for epoch %v failed: %w", epoch, err)
	} else if rows == 0 {
		return time.Time{}, fmt.Errorf("%w: no request time for epoch %v", sql.ErrNotFound, epoch)
	}
	return timestamp, nil
}

func Clear(db sql.Executor) error {
	_, err := db.Exec(`delete from atx_sync_state`, nil, nil)
	if err != nil {
		return fmt.Errorf("clear atx sync state failed: %w", err)
	}
	_, err = db.Exec(`delete from atx_sync_requests`, nil, nil)
	if err != nil {
		return fmt.Errorf("clear atx sync requests failed: %w", err)
	}
	return nil
}
