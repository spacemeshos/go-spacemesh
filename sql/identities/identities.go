package identities

import (
	"context"
	"fmt"
	"time"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/malfeasance/wire"
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
func GetMalfeasanceProof(db sql.Executor, nodeID types.NodeID) (*wire.MalfeasanceProof, error) {
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
	var proof wire.MalfeasanceProof
	if err = codec.Decode(data, &proof); err != nil {
		return nil, err
	}
	proof.SetReceived(received.Local())
	return &proof, nil
}

// GetBlobSizes returns the sizes of the blobs corresponding to malfeasance proofs for the
// specified identities. For non-existent proofs, the corresponding items are set to -1.
func GetBlobSizes(db sql.Executor, ids [][]byte) (sizes []int, err error) {
	return sql.GetBlobSizes(db, "select pubkey, length(proof) from identities where pubkey in", ids)
}

// LoadMalfeasanceBlob returns the malfeasance proof in raw bytes for the given identity.
func LoadMalfeasanceBlob(ctx context.Context, db sql.Executor, nodeID []byte, blob *sql.Blob) error {
	return sql.LoadBlob(db, "select proof from identities where pubkey = ?1;", nodeID, blob)
}

// IterateMalicious invokes the specified callback for each malicious node ID.
// It stops if the callback returns an error.
func IterateMalicious(
	db sql.Executor,
	callback func(total int, id types.NodeID) error,
) error {
	var callbackErr error
	dec := func(stmt *sql.Statement) bool {
		var nid types.NodeID
		total := stmt.ColumnInt(0)
		stmt.ColumnBytes(1, nid[:])
		if callbackErr = callback(total, nid); callbackErr != nil {
			return false
		}
		return true
	}

	// Get total count in the same select statement to avoid the need for transaction
	if _, err := db.Exec(
		"select (select count(*) from identities where proof is not null) as total, "+
			"pubkey from identities where proof is not null", nil, dec); err != nil {
		return fmt.Errorf("get malicious identities: %w", err)
	}

	return callbackErr
}

// GetMalicious retrieves malicious node IDs from the database.
func GetMalicious(db sql.Executor) (nids []types.NodeID, err error) {
	if err = IterateMalicious(db, func(total int, nid types.NodeID) error {
		if nids == nil {
			nids = make([]types.NodeID, 0, total)
		}
		nids = append(nids, nid)
		return nil
	}); err != nil {
		return nil, err
	}
	if len(nids) != cap(nids) {
		panic("BUG: bad malicious node ID count")
	}
	return nids, nil
}
