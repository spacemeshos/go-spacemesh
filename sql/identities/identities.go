package identities

import (
	"context"
	"fmt"
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/builder"
)

// SetMalicious records identity as malicious.
func SetMalicious(db sql.Executor, nodeID types.NodeID, proof []byte, received time.Time) error {
	_, err := db.Exec(`
		insert into identities (pubkey, proof, received)
		values (?1, ?2, ?3)
		on conflict do nothing
	`, func(stmt *sql.Statement) {
		stmt.BindBytes(1, nodeID.Bytes())
		stmt.BindBytes(2, proof)
		stmt.BindInt64(3, received.UnixNano())
	}, nil)
	if err != nil {
		return fmt.Errorf("set malicious %v: %w", nodeID, err)
	}
	return nil
}

// IsMalicious returns true if identity is known to be malicious.
func IsMalicious(db sql.Executor, nodeID types.NodeID) (bool, error) {
	rows, err := db.Exec(
		"select 1 from identities where pubkey = ?1;",
		func(stmt *sql.Statement) {
			stmt.BindBytes(1, nodeID.Bytes())
		}, nil)
	if err != nil {
		return false, fmt.Errorf("is malicious %v: %w", nodeID, err)
	}
	return rows > 0, nil
}

// GetBlobSizes returns the sizes of the blobs corresponding to malfeasance proofs for the
// specified identities. For non-existent proofs, the corresponding items are set to -1.
func GetBlobSizes(db sql.Executor, ids [][]byte) (sizes []int, err error) {
	return sql.GetBlobSizes(db, "select pubkey, length(proof) from identities where pubkey in", ids)
}

// LoadMalfeasanceBlob returns the malfeasance proof in raw bytes for the given identity.
func LoadMalfeasanceBlob(_ context.Context, db sql.Executor, nodeID []byte, blob *sql.Blob) error {
	err := sql.LoadBlob(db, "select proof from identities where pubkey = ?1;", nodeID, blob)
	if err == nil && len(blob.Bytes) == 0 {
		return sql.ErrNotFound
	}
	return err
}

func IterateMaliciousOps(
	db sql.Executor,
	operations builder.Operations,
	fn func(types.NodeID, []byte, time.Time) bool,
) error {
	_, err := db.Exec(
		"select pubkey, proof, received from identities"+builder.FilterFrom(operations),
		builder.BindingsFrom(operations),
		func(stmt *sql.Statement) bool {
			var id types.NodeID
			stmt.ColumnBytes(0, id[:])
			proof := make([]byte, stmt.ColumnLen(1))
			stmt.ColumnBytes(1, proof)
			received := time.Unix(0, stmt.ColumnInt64(2))
			return fn(id, proof, received)
		},
	)
	return err
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
func GetMalicious(db sql.Executor) (ids []types.NodeID, err error) {
	if err = IterateMalicious(db, func(total int, nid types.NodeID) error {
		if ids == nil {
			ids = make([]types.NodeID, 0, total)
		}
		ids = append(ids, nid)
		return nil
	}); err != nil {
		return nil, err
	}
	if len(ids) != cap(ids) {
		panic("BUG: bad malicious node ID count")
	}
	return ids, nil
}
