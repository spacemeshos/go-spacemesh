package kvstore

import (
	"fmt"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
)

const commitmentATXKey = "commitmentATX"

func getKeyForNode(nodeId types.NodeID) string {
	return fmt.Sprintf("%s-%s", commitmentATXKey, nodeId)
}

// AddCommitmentATXForNode adds the id for the commitment atx to the key-value store.
func AddCommitmentATXForNode(db sql.Executor, atx types.ATXID, nodeId types.NodeID) error {
	key := getKeyForNode(nodeId)
	bytes, err := codec.Encode(&atx)
	if err != nil {
		return fmt.Errorf("failed encoding: %w", err)
	}

	if _, err := db.Exec(`insert into kvstore (id, value) values (?1, ?2)`,
		func(stmt *sql.Statement) {
			stmt.BindBytes(1, []byte(key))
			stmt.BindBytes(2, bytes)
		}, nil); err != nil {
		return fmt.Errorf("failed to insert value: %w", err)
	}

	return nil
}

// GetCommitmentATXForNode returns the id for the commitment atx from the key-value store.
func GetCommitmentATXForNode(db sql.Executor, nodeId types.NodeID) (types.ATXID, error) {
	key := getKeyForNode(nodeId)
	var val []byte
	if rows, err := db.Exec("select value from kvstore where id = ?1;", func(stmt *sql.Statement) {
		stmt.BindBytes(1, []byte(key))
	}, func(stmt *sql.Statement) bool {
		val = make([]byte, stmt.ColumnLen(0))
		stmt.ColumnBytes(0, val[:])
		return true
	}); err != nil {
		return *types.EmptyATXID, fmt.Errorf("failed to get value: %w", err)
	} else if rows == 0 {
		return *types.EmptyATXID, fmt.Errorf("failed to get value: %w", sql.ErrNotFound)
	}

	var res types.ATXID
	if err := codec.Decode(val, &res); err != nil {
		return *types.EmptyATXID, fmt.Errorf("failed decoding: %w", err)
	}
	return res, nil
}
