package identities

import (
	"fmt"
	"io"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
)

// SetMalicious records identity as malicious.
func SetMalicious(db sql.Executor, nodeID types.NodeID, proof []byte) error {
	_, err := db.Exec(`insert into identities (pubkey, proof)
	values (?1, ?2)
	on conflict do nothing;`,
		func(stmt *sql.Statement) {
			stmt.BindBytes(1, nodeID.Bytes())
			stmt.BindBytes(2, proof)
		}, nil,
	)
	if err != nil {
		return fmt.Errorf("set malicious %v: %w", nodeID, err)
	}
	return nil
}

// IsMalicious returns true if identity is known to be malicious.
func IsMalicious(db sql.Executor, nodeID types.NodeID) (bool, error) {
	rows, err := db.Exec("select 1 from identities where pubkey = ?1;",
		func(stmt *sql.Statement) {
			stmt.BindBytes(1, nodeID.Bytes())
		}, nil)
	if err != nil {
		return false, fmt.Errorf("is malicious %v: %w", nodeID, err)
	}
	return rows > 0, nil
}

func GetMalfeasanceProof(db sql.Executor, nodeID types.NodeID) (*types.MalfeasanceProof, error) {
	var (
		proof types.MalfeasanceProof
		err   error
		n     int
	)
	rows, err := db.Exec("select proof from identities where pubkey = ?1;",
		func(stmt *sql.Statement) {
			stmt.BindBytes(1, nodeID.Bytes())
		}, func(stmt *sql.Statement) bool {
			if n, err = codec.DecodeFrom(stmt.ColumnReader(0), &proof); err != nil {
				if err != io.EOF {
					err = fmt.Errorf("get proof nodeID %v: %w", nodeID, err)
					return false
				}
			} else if n == 0 {
				err = fmt.Errorf("proof data missing nodeID %v", nodeID)
				return false
			}
			return true
		})
	if err != nil {
		return nil, fmt.Errorf("proof %v: %w", nodeID, err)
	}
	if rows == 0 {
		return nil, sql.ErrNotFound
	}
	return &proof, nil
}
