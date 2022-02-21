package refballots

import (
	"fmt"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
)

// Get gets a ref ballot for a given epoch.
func Get(db sql.Executor, epoch types.EpochID) (refBallot types.BallotID, err error) {
	enc := func(stmt *sql.Statement) {
		stmt.BindInt64(1, int64(epoch))
	}
	dec := func(stmt *sql.Statement) bool {
		stmt.ColumnBytes(0, refBallot[:])
		return true
	}

	rows, err := db.Exec("select ballot_id from ref_ballots where epoch = ?1", enc, dec)
	if err != nil {
		return types.BallotID{}, fmt.Errorf("epoch %v: %w", epoch, err)
	}
	if rows == 0 {
		return types.BallotID{}, fmt.Errorf("epoch %s: %w", epoch, sql.ErrNotFound)
	}

	return refBallot, nil
}

// Add adds a ref ballot for a given epoch.
func Add(db sql.Executor, epoch types.EpochID, refBallot types.BallotID) error {
	enc := func(stmt *sql.Statement) {
		stmt.BindInt64(1, int64(epoch))
		stmt.BindBytes(2, refBallot.Bytes())
	}
	_, err := db.Exec("insert into ref_ballots (epoch, ballot_id) values (?1, ?2);", enc, nil)
	if err != nil {
		return fmt.Errorf("insert epoch %v, ref ballot %v: %w", epoch, refBallot, err)
	}

	return nil
}
