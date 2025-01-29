package localatxs

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
)

func AddAtx(
	db sql.LocalDatabase,
	epoch types.EpochID,
	id types.ATXID,
	nodeID types.NodeID,
	blob []byte,
	poet types.PoetProofRef,
) error {
	_, err := db.Exec("INSERT INTO published_atxs (epoch, id, pubkey, atx, poetref) VALUES (?1, ?2, ?3, ?4, ?5)",
		func(s *sql.Statement) {
			s.BindInt64(1, int64(epoch))
			s.BindBytes(2, id[:])
			s.BindBytes(3, nodeID[:])
			s.BindBytes(4, blob)
			s.BindBytes(5, poet[:])
		}, nil)
	return err
}

func AtxAndPoet(
	db sql.LocalDatabase,
	epoch types.EpochID,
	nodeID types.NodeID,
) (id types.ATXID, blob []byte, poet types.PoetProofRef, err error) {
	rows, err := db.Exec("select id, atx, poetRef from published_atxs where epoch = ?1 and pubkey = ?2",
		func(s *sql.Statement) {
			s.BindInt64(1, int64(epoch))
			s.BindBytes(2, nodeID[:])
		},
		func(s *sql.Statement) bool {
			s.ColumnBytes(0, id[:])
			blob = make([]byte, s.ColumnLen(1))
			s.ColumnBytes(1, blob)
			s.ColumnBytes(2, poet[:])
			return false
		},
	)
	if rows == 0 {
		return id, blob, poet, sql.ErrNotFound
	}

	return id, blob, poet, err
}
