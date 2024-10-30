package atxsync

import (
	"fmt"
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
)

type EpochSyncState = map[types.ATXID]*IDSyncState

func fromDatabase(tries int) *IDSyncState {
	return &IDSyncState{
		Tries:          tries,
		persisted:      true,
		persistedTries: tries,
	}
}

// IDSyncState tracks how many times we tried to sync an ATX.
type IDSyncState struct {
	Tries          int
	persisted      bool
	persistedTries int
}

// TriesPersisted returns true if the number of tries is equal to the number of persisted tries.
func (s *IDSyncState) TriesPersisted() bool {
	return s.persisted && s.persistedTries == s.Tries
}

// SetPersisted sets the number of tries to the number of persisted tries.
func (s *IDSyncState) setPersisted() {
	s.persisted = true
	s.persistedTries = s.Tries
}

func GetSyncState(db sql.Executor, epoch types.EpochID) (EpochSyncState, error) {
	states := EpochSyncState{}
	_, err := db.Exec("select id, requests from atx_sync_state where epoch = ?1",
		func(stmt *sql.Statement) {
			stmt.BindInt64(1, int64(epoch))
		}, func(stmt *sql.Statement) bool {
			var id types.ATXID
			stmt.ColumnBytes(0, id[:])
			states[id] = fromDatabase(stmt.ColumnInt(1))
			return true
		})
	if err != nil {
		return nil, fmt.Errorf("select synced atx ids for epoch failed %v: %w", epoch, err)
	}
	return states, nil
}

func SaveSyncState(db sql.Executor, epoch types.EpochID, states EpochSyncState, max int) error {
	for id, requests := range states {
		var err error
		if requests.Tries == max {
			_, err = db.Exec(`delete from atx_sync_state where epoch = ?1 and id = ?2`, func(stmt *sql.Statement) {
				stmt.BindInt64(1, int64(epoch))
				stmt.BindBytes(2, id[:])
			}, nil)
		} else if !requests.TriesPersisted() {
			_, err = db.Exec(`insert into atx_sync_state
		(epoch, id, requests) values (?1, ?2, ?3)
		on conflict(epoch, id) do update set requests = ?3;`,
				func(stmt *sql.Statement) {
					stmt.BindInt64(1, int64(epoch))
					stmt.BindBytes(2, id[:])
					stmt.BindInt64(3, int64(requests.Tries))
				}, nil)
		}
		requests.setPersisted()
		if err != nil {
			return fmt.Errorf("insert synced atx id %v/%v failed: %w", epoch, id.ShortString(), err)
		}
	}
	return nil
}

func SaveRequest(db sql.Executor, epoch types.EpochID, timestamp time.Time, total, downloaded int64) error {
	_, err := db.Exec(`insert into atx_sync_requests
	(epoch, timestamp, total, downloaded) values (?1, ?2, ?3, ?4)
	on conflict(epoch) do update set timestamp = ?2, total = ?3, downloaded = ?4;`,
		func(stmt *sql.Statement) {
			stmt.BindInt64(1, int64(epoch))
			stmt.BindInt64(2, timestamp.Unix())
			stmt.BindInt64(3, total)
			stmt.BindInt64(4, downloaded)
		}, nil)
	if err != nil {
		return fmt.Errorf("insert request time for epoch %v failed: %w", epoch, err)
	}
	return nil
}

func GetRequest(db sql.Executor, epoch types.EpochID) (time.Time, int64, int64, error) {
	var (
		timestamp  time.Time
		total      int64
		downloaded int64
	)
	rows, err := db.Exec("select timestamp, total, downloaded from atx_sync_requests where epoch = ?1",
		func(stmt *sql.Statement) {
			stmt.BindInt64(1, int64(epoch))
		}, func(stmt *sql.Statement) bool {
			timestamp = time.Unix(stmt.ColumnInt64(0), 0)
			total = stmt.ColumnInt64(1)
			downloaded = stmt.ColumnInt64(2)
			return true
		})
	if err != nil {
		return time.Time{}, 0, 0, fmt.Errorf("select request time for epoch %v failed: %w", epoch, err)
	} else if rows == 0 {
		return time.Time{}, 0, 0, fmt.Errorf("%w: no request time for epoch %v", sql.ErrNotFound, epoch)
	}
	return timestamp, total, downloaded, nil
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
