package beacons

import (
	"fmt"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
)

// Get gets a beacon for a given epoch.
func Get(db sql.Executor, epoch types.EpochID) (beacon types.Beacon, err error) {
	enc := func(stmt *sql.Statement) {
		stmt.BindInt64(1, int64(epoch))
	}
	dec := func(stmt *sql.Statement) bool {
		stmt.ColumnBytes(0, beacon[:])
		return true
	}

	rows, err := db.Exec("select beacon from beacons where epoch = ?1", enc, dec)
	if err != nil {
		return types.Beacon{}, fmt.Errorf("epoch %v: %w", epoch, err)
	}
	if rows == 0 {
		return types.Beacon{}, fmt.Errorf("epoch %s: %w", epoch, sql.ErrNotFound)
	}

	return beacon, nil
}

// Add adds a beacon for a given epoch.
func Add(db sql.Executor, epoch types.EpochID, beacon types.Beacon) error {
	enc := func(stmt *sql.Statement) {
		stmt.BindInt64(1, int64(epoch))
		stmt.BindBytes(2, beacon.Bytes())
	}
	_, err := db.Exec("insert into beacons (epoch, beacon) values (?1, ?2);", enc, nil)
	if err != nil {
		return fmt.Errorf("insert epoch %v, beacon %v: %w", epoch, beacon, err)
	}

	return nil
}

func AddOverwrite(db sql.Executor, epoch types.EpochID, beacon types.Beacon) error {
	enc := func(stmt *sql.Statement) {
		stmt.BindInt64(1, int64(epoch))
		stmt.BindBytes(2, beacon.Bytes())
	}
	_, err := db.Exec(`insert into beacons (epoch, beacon) values (?1, ?2)
		on conflict do update set beacon = ?2;`, enc, nil)
	if err != nil {
		return fmt.Errorf("insert epoch %v, beacon %v: %w", epoch, beacon, err)
	}

	return nil
}
