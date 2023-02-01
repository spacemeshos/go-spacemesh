package identities

import (
	"fmt"

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

// GetMalfeasanceProof returns the malfeasance proof for the given identity.
func GetMalfeasanceProof(db sql.Executor, nodeID types.NodeID) (*types.MalfeasanceProof, error) {
	data, err := GetMalfeasanceBlob(db, nodeID.Bytes())
	if err != nil {
		return nil, err
	}
	var proof types.MalfeasanceProof
	if err = codec.Decode(data, &proof); err != nil {
		return nil, err
	}
	return &proof, nil
}

// GetMalfeasanceBlob returns the malfeasance proof in raw bytes for the given identity.
func GetMalfeasanceBlob(db sql.Executor, nodeID []byte) ([]byte, error) {
	var (
		proof []byte
		err   error
	)
	rows, err := db.Exec("select proof from identities where pubkey = ?1;",
		func(stmt *sql.Statement) {
			stmt.BindBytes(1, nodeID)
		}, func(stmt *sql.Statement) bool {
			proof = make([]byte, stmt.ColumnLen(0))
			stmt.ColumnBytes(0, proof[:])
			return true
		})
	if err != nil {
		return nil, fmt.Errorf("proof blob %v: %w", nodeID, err)
	}
	if rows == 0 {
		return nil, sql.ErrNotFound
	}
	return proof, nil
}

func GetMalicious(db sql.Executor) ([]types.NodeID, error) {
	var (
		result []types.NodeID
		err    error
	)
	_, err = db.Exec("select pubkey from identities where proof is not null;",
		nil,
		func(stmt *sql.Statement) bool {
			var nid types.NodeID
			stmt.ColumnBytes(0, nid[:])
			result = append(result, nid)
			return true
		})
	if err != nil {
		return nil, fmt.Errorf("get malicious identities: %w", err)
	}
	return result, nil
}
