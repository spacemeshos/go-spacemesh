package marriage

import (
	"fmt"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
)

type ID int

type Info struct {
	ID            ID
	NodeID        types.NodeID
	ATX           types.ATXID
	MarriageIndex int
	Target        types.NodeID
	Signature     types.EdSignature
}

// NewID returns a new unique ID for a marriage. The ID is unique across all marriages.
// Use within a transaction to ensure that the returned ID is not used by another routine.
func NewID(db sql.Executor) (ID, error) {
	var id ID
	_, err := db.Exec(`
	SELECT max(id) FROM marriages
	`, nil, func(stmt *sql.Statement) bool {
		id = ID(stmt.ColumnInt64(0) + 1)
		return false
	})
	if err != nil {
		return 0, fmt.Errorf("getting max id: %w", err)
	}
	return id, nil
}

// Add inserts a nodeID to the marriages table for the given MarriageID.
// If the nodeID already exists in the table, it is updated.
// Updates cannot change the MarriageID.
func Add(db sql.Executor, marriage Info) error {
	rows, err := db.Exec(`
		INSERT INTO marriages (id, pubkey, marriage_atx, marriage_idx, marriage_target, marriage_sig)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT(pubkey) DO UPDATE SET
			marriage_atx = $3,
			marriage_idx = $4,
			marriage_target = $5,
			marriage_sig = $6
		WHERE id = $1
		RETURNING 1
	`, func(s *sql.Statement) {
		s.BindInt64(1, int64(marriage.ID))
		s.BindBytes(2, marriage.NodeID.Bytes())
		s.BindBytes(3, marriage.ATX.Bytes())
		s.BindInt64(4, int64(marriage.MarriageIndex))
		s.BindBytes(5, marriage.Target.Bytes())
		s.BindBytes(6, marriage.Signature.Bytes())
	}, nil)
	if err != nil {
		return fmt.Errorf("updating marriage set: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("%w: tried to change marriage id for %s", sql.ErrConflict, marriage.NodeID)
	}
	return nil
}

func UpdateID(db sql.Executor, nodeID types.NodeID, id ID) error {
	_, err := db.Exec(`
		UPDATE marriages
		SET id = $1
		WHERE pubkey = $2
	`, func(s *sql.Statement) {
		s.BindInt64(1, int64(id))
		s.BindBytes(2, nodeID.Bytes())
	}, nil)
	if err != nil {
		return fmt.Errorf("updating marriage id: %w", err)
	}
	return nil
}

func FindIDByNodeID(db sql.Executor, nodeID types.NodeID) (ID, error) {
	var id ID
	rows, err := db.Exec(`
		SELECT id
		FROM marriages
		WHERE pubkey = $1
	`, func(s *sql.Statement) {
		s.BindBytes(1, nodeID.Bytes())
	}, func(s *sql.Statement) bool {
		id = ID(s.ColumnInt64(0))
		return false
	})
	if err != nil {
		return 0, fmt.Errorf("selecting marriage id: %w", err)
	}
	if rows == 0 {
		return 0, sql.ErrNotFound
	}
	return id, nil
}

func FindByNodeID(db sql.Executor, nodeID types.NodeID) (Info, error) {
	var m Info
	rows, err := db.Exec(`
		SELECT id, marriage_atx, marriage_idx, marriage_target, marriage_sig
		FROM marriages
		WHERE pubkey = $1
	`, func(s *sql.Statement) {
		s.BindBytes(1, nodeID.Bytes())
	}, func(s *sql.Statement) bool {
		m.NodeID = nodeID
		m.ID = ID(s.ColumnInt64(0))
		s.ColumnBytes(1, m.ATX[:])
		m.MarriageIndex = int(s.ColumnInt64(2))
		s.ColumnBytes(3, m.Target[:])
		s.ColumnBytes(4, m.Signature[:])
		return false
	})
	if err != nil {
		return m, fmt.Errorf("selecting marriage: %w", err)
	}
	if rows == 0 {
		return m, sql.ErrNotFound
	}
	return m, nil
}

func NodeIDsByID(db sql.Executor, id ID) ([]types.NodeID, error) {
	var nodeIDs []types.NodeID
	_, err := db.Exec(`
		SELECT pubkey
		FROM marriages
		WHERE id = $1
	`, func(s *sql.Statement) {
		s.BindInt64(1, int64(id))
	}, func(s *sql.Statement) bool {
		var nodeID types.NodeID
		s.ColumnBytes(0, nodeID[:])
		nodeIDs = append(nodeIDs, nodeID)
		return true
	})
	if err != nil {
		return nil, fmt.Errorf("select node IDs: %w", err)
	}
	return nodeIDs, nil
}

func Iterate(db sql.Executor, cb func(data Info) bool) error {
	_, err := db.Exec(`
		SELECT id, pubkey, marriage_atx, marriage_idx, marriage_target, marriage_sig
		FROM marriages
	`, nil, func(stmt *sql.Statement) bool {
		var data Info
		data.ID = ID(stmt.ColumnInt64(0))
		stmt.ColumnBytes(1, data.NodeID[:])
		stmt.ColumnBytes(2, data.ATX[:])
		data.MarriageIndex = int(stmt.ColumnInt64(3))
		stmt.ColumnBytes(4, data.Target[:])
		stmt.ColumnBytes(5, data.Signature[:])
		return cb(data)
	})
	return err
}
