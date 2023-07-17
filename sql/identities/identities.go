package identities

import (
	"fmt"
	"time"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
)

// SetMalicious records identity as malicious.
func SetMalicious(db sql.Executor, nodeID types.NodeID, proof []byte, received time.Time) error {
	_, err := db.Exec(`insert into identities (pubkey, proof, received)
	values (?1, ?2, ?3)
	on conflict do nothing;`,
		func(stmt *sql.Statement) {
			stmt.BindBytes(1, nodeID.Bytes())
			stmt.BindBytes(2, proof)
			stmt.BindInt64(3, received.UnixNano())
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
	var (
		data     []byte
		received time.Time
	)
	rows, err := db.Exec("select proof, received from identities where pubkey = ?1;",
		func(stmt *sql.Statement) {
			stmt.BindBytes(1, nodeID.Bytes())
		}, func(stmt *sql.Statement) bool {
			data = make([]byte, stmt.ColumnLen(0))
			stmt.ColumnBytes(0, data[:])
			received = time.Unix(0, stmt.ColumnInt64(1)).Local()
			return true
		})
	if err != nil {
		return nil, fmt.Errorf("proof %v: %w", nodeID, err)
	}
	if rows == 0 {
		return nil, sql.ErrNotFound
	}
	var proof types.MalfeasanceProof
	if err = codec.Decode(data, &proof); err != nil {
		return nil, err
	}
	proof.SetReceived(received.Local())
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
