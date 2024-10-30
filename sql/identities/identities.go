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
		INSERT INTO identities (pubkey, proof, received)
		VALUES (?1, ?2, ?3)
		ON CONFLICT(pubkey) DO NOTHING
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
	rows, err := db.Exec(`
		SELECT 1
		FROM identities
		WHERE pubkey = ?1
	`, func(stmt *sql.Statement) {
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
	return sql.GetBlobSizes(db, "SELECT pubkey, length(proof) FROM identities WHERE pubkey IN", ids)
}

// LoadMalfeasanceBlob returns the malfeasance proof in raw bytes for the given identity.
func LoadMalfeasanceBlob(_ context.Context, db sql.Executor, nodeID []byte, blob *sql.Blob) error {
	return sql.LoadBlob(db, "SELECT proof FROM identities WHERE pubkey = ?1;", nodeID, blob)
}

func IterateOps(
	db sql.Executor,
	operations builder.Operations,
	fn func(types.NodeID, []byte, time.Time) bool,
) error {
	_, err := db.Exec(`
		SELECT pubkey, proof, received
		FROM identities
	`+builder.FilterFrom(operations), builder.BindingsFrom(operations),
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

// AllMalicious retrieves malicious node IDs from the database.
func AllMalicious(db sql.Executor) ([]types.NodeID, error) {
	var ids []types.NodeID
	if _, err := db.Exec(`
		SELECT (SELECT COUNT(*) FROM identities) AS total, pubkey
		FROM identities
	`, nil, func(stmt *sql.Statement) bool {
		if ids == nil {
			ids = make([]types.NodeID, 0, stmt.ColumnInt(0))
		}
		var id types.NodeID
		stmt.ColumnBytes(1, id[:])
		ids = append(ids, id)
		return true
	}); err != nil {
		return nil, err
	}
	if len(ids) != cap(ids) {
		panic("BUG: bad malicious node ID count")
	}
	return ids, nil
}
