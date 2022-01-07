package ballots

import (
	"bytes"
	"fmt"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
)

func decodeBallot(id types.BallotID, sig, pubkey, body *bytes.Reader) (*types.Ballot, error) {
	sigBytes := make([]byte, sig.Len())
	if _, err := sig.Read(sigBytes); err != nil {
		return nil, err
	}
	pubkeyBytes := make([]byte, pubkey.Len())
	if _, err := pubkey.Read(pubkeyBytes); err != nil {
		return nil, err
	}
	inner := types.InnerBallot{}
	if _, err := codec.DecodeFrom(body, &inner); err != nil {
		return nil, err
	}
	ballot := types.NewExistingBallot(id, sigBytes, pubkeyBytes, inner)
	return &ballot, nil
}

// Add ballot to the database.
func Add(db sql.Executor, ballot *types.Ballot) error {
	bytes, err := codec.Encode(ballot.InnerBallot)
	if err != nil {
		return err
	}
	return db.Exec("insert or ignore into ballots (id, layer, signature, pubkey, ballot) values (?1, ?2, ?3, ?4, ?5);",
		func(stmt *sql.Statement) {
			stmt.BindBytes(1, ballot.ID().Bytes())
			stmt.BindInt64(2, int64(ballot.LayerIndex.Value))
			stmt.BindBytes(3, ballot.Signature)
			stmt.BindBytes(4, ballot.SmesherID().Bytes())
			stmt.BindBytes(5, bytes)
		}, nil)
}

// Get ballot with id from database.
func Get(db sql.Executor, id types.BallotID) (rst *types.Ballot, err error) {
	if err := db.Exec("select (signature, pubkey, ballot) from ballots where id = ?1;", func(stmt *sql.Statement) {
		stmt.BindBytes(1, id.Bytes())
	}, func(stmt *sql.Statement) bool {
		rst, err = decodeBallot(id, stmt.ColumnReader(0), stmt.ColumnReader(1), stmt.ColumnReader(2))
		return true
	}); err != nil {
		return nil, fmt.Errorf("get %s: %w", id, err)
	}
	return rst, err
}

// ForLayerFull returns full body ballot for layer.
func ForLayerFull(db sql.Executor, lid types.LayerID) (rst []*types.Ballot, err error) {
	if err := db.Exec("select (id, signature, pubkey, ballot) from ballots where layer = ?1;", func(stmt *sql.Statement) {
		stmt.BindBytes(1, lid.Bytes())
	}, func(stmt *sql.Statement) bool {
		id := types.BallotID{}
		stmt.BindBytes(0, id[:])
		var ballot *types.Ballot
		ballot, err = decodeBallot(id, stmt.ColumnReader(1), stmt.ColumnReader(2), stmt.ColumnReader(3))
		if err != nil {
			return false
		}
		rst = append(rst, ballot)
		return true
	}); err != nil {
		return nil, fmt.Errorf("ballots for layer %s: %w", lid, err)
	}
	return rst, err
}
