package atxs

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
)

func AddBlob(db sql.LocalDatabase, epoch types.EpochID, id types.ATXID, nodeID types.NodeID, blob []byte) error {
	_, err := db.Exec("INSERT INTO atx_blobs (epoch, id, pubkey, atx) VALUES (?1, ?2, ?3, ?4)",
		func(s *sql.Statement) {
			s.BindInt64(1, int64(epoch))
			s.BindBytes(2, id[:])
			s.BindBytes(3, nodeID[:])
			s.BindBytes(4, blob)
		}, nil)
	return err
}

func AtxBlob(db sql.LocalDatabase, epoch types.EpochID, nodeID types.NodeID) (id types.ATXID, blob []byte, err error) {
	rows, err := db.Exec("select id, atx from atx_blobs where epoch = ?1 and pubkey = ?2",
		func(s *sql.Statement) {
			s.BindInt64(1, int64(epoch))
			s.BindBytes(2, nodeID[:])
		},
		func(s *sql.Statement) bool {
			s.ColumnBytes(0, id[:])
			blob = make([]byte, s.ColumnLen(1))
			s.ColumnBytes(1, blob)
			return false
		},
	)
	if rows == 0 {
		return id, blob, sql.ErrNotFound
	}

	return id, blob, err
}
