package nipost

import (
	"fmt"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/sql"
)

func AddChallenge(db *datastore.LocalDB, nodeID types.NodeID, ch *types.NIPostChallenge) error {
	enc := func(stmt *sql.Statement) {
		stmt.BindBytes(1, nodeID.Bytes())
		stmt.BindInt64(2, int64(ch.PublishEpoch))
		stmt.BindInt64(3, int64(ch.Sequence))
		stmt.BindBytes(4, ch.PrevATXID.Bytes())
		stmt.BindBytes(5, ch.PositioningATX.Bytes())
		if ch.CommitmentATX != nil {
			stmt.BindBytes(6, ch.CommitmentATX.Bytes())
		} else {
			stmt.BindNull(6)
		}
		if ch.InitialPost != nil {
			stmt.BindInt64(7, int64(ch.InitialPost.Nonce))
			stmt.BindBytes(8, ch.InitialPost.Indices)
			stmt.BindInt64(9, int64(ch.InitialPost.Pow))
		} else {
			stmt.BindInt64(7, -1)
			stmt.BindNull(8)
			stmt.BindInt64(9, -1)
		}
	}

	if _, err := db.Exec(`
		insert into nipost (id, epoch, sequence, prev_atx, pos_atx, commit_atx, post_nonce, post_indices, post_pow)
		values (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8, ?9);`, enc, nil,
	); err != nil {
		return fmt.Errorf("insert nipost challenge for %s pub-epoch %d: %w", nodeID, ch.PublishEpoch, err)
	}
	return nil
}

func UpdateChallenge(db *datastore.LocalDB, nodeID types.NodeID, ch *types.NIPostChallenge) error {
	enc := func(stmt *sql.Statement) {
		stmt.BindBytes(1, nodeID.Bytes())
		stmt.BindInt64(2, int64(ch.PublishEpoch))
		stmt.BindInt64(3, int64(ch.Sequence))
		stmt.BindBytes(4, ch.PrevATXID.Bytes())
		stmt.BindBytes(5, ch.PositioningATX.Bytes())
		if ch.CommitmentATX != nil {
			stmt.BindBytes(6, ch.CommitmentATX.Bytes())
		} else {
			stmt.BindNull(6)
		}
		if ch.InitialPost != nil {
			stmt.BindInt64(7, int64(ch.InitialPost.Nonce))
			stmt.BindBytes(8, ch.InitialPost.Indices)
			stmt.BindInt64(9, int64(ch.InitialPost.Pow))
		} else {
			stmt.BindInt64(7, -1)
			stmt.BindNull(8)
			stmt.BindInt64(9, -1)
		}
	}

	if _, err := db.Exec(`
		update nipost set epoch = ?2, sequence = ?3, prev_atx = ?4, pos_atx = ?5,
		commit_atx = ?6, post_nonce = ?7, post_indices = ?8, post_pow = ?9
		where id = ?1;`, enc, nil,
	); err != nil {
		return fmt.Errorf("update nipost challenge for %s pub-epoch %d: %w", nodeID, ch.PublishEpoch, err)
	}
	return nil
}

// RemoveChallenge removes the NIPost challenge for the given nodeID.
func RemoveChallenge(db *datastore.LocalDB, nodeID types.NodeID) error {
	enc := func(stmt *sql.Statement) {
		stmt.BindBytes(1, nodeID.Bytes())
	}
	if _, err := db.Exec(`delete from nipost where id = ?1;`, enc, nil); err != nil {
		return fmt.Errorf("remove nipost challenge for %s: %w", nodeID, err)
	}
	return nil
}

// Challenge returns the NIPost challenge for the given nodeID.
func Challenge(db *datastore.LocalDB, nodeID types.NodeID) (*types.NIPostChallenge, error) {
	var ch *types.NIPostChallenge
	enc := func(stmt *sql.Statement) {
		stmt.BindBytes(1, nodeID.Bytes())
	}
	dec := func(stmt *sql.Statement) bool {
		ch = &types.NIPostChallenge{}
		ch.PublishEpoch = types.EpochID(stmt.ColumnInt64(0))
		ch.Sequence = uint64(stmt.ColumnInt64(1))
		stmt.ColumnBytes(2, ch.PrevATXID[:])
		stmt.ColumnBytes(3, ch.PositioningATX[:])
		ch.CommitmentATX = &types.ATXID{}
		if n := stmt.ColumnBytes(4, ch.CommitmentATX[:]); n == 0 {
			ch.CommitmentATX = nil
		}
		ch.InitialPost = &types.Post{}
		if n := stmt.ColumnLen(6); n > 0 {
			ch.InitialPost.Nonce = uint32(stmt.ColumnInt64(5))
			ch.InitialPost.Indices = make([]byte, n)
			stmt.ColumnBytes(6, ch.InitialPost.Indices)
			ch.InitialPost.Pow = uint64(stmt.ColumnInt64(7))
			return true
		}

		ch.InitialPost = nil
		return true
	}
	query := `select epoch, sequence, prev_atx, pos_atx, commit_atx, post_nonce, post_indices, post_pow
	 from nipost where id = ?1 limit 1;`
	_, err := db.Exec(query, enc, dec)
	if err != nil {
		return nil, fmt.Errorf("get challenge from node id %s: %w", nodeID.ShortString(), err)
	}
	if ch == nil {
		return nil, fmt.Errorf("get challenge from node id %s: %w", nodeID.ShortString(), sql.ErrNotFound)
	}
	return ch, nil
}
