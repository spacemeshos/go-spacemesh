package malfeasance

import (
	"context"
	"fmt"
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
)

func Add(db sql.Executor, nodeID types.NodeID, domain byte, proof []byte, received time.Time) error {
	_, err := db.Exec(`
		INSERT INTO malfeasance (pubkey, received, domain, proof)
		VALUES (?1, ?2, ?3, ?4);`,
		func(stmt *sql.Statement) {
			stmt.BindBytes(1, nodeID.Bytes())
			stmt.BindInt64(2, received.UnixNano())
			stmt.BindInt64(3, int64(domain))
			stmt.BindBytes(4, proof)
		}, nil,
	)
	if err != nil {
		return fmt.Errorf("add malfeasance %s: %w", nodeID, err)
	}
	return nil
}

func AddMarried(db sql.Executor, nodeID, marriedTo types.NodeID, received time.Time) error {
	_, err := db.Exec(`
		INSERT INTO malfeasance (pubkey, received, married_to)
		VALUES (?1, ?2, ?3);`,
		func(stmt *sql.Statement) {
			stmt.BindBytes(1, nodeID.Bytes())
			stmt.BindInt64(2, received.UnixNano())
			stmt.BindBytes(3, marriedTo.Bytes())
		}, nil,
	)
	if err != nil {
		return fmt.Errorf("add married %s: %w", nodeID, err)
	}
	return nil
}

func IsMalicious(db sql.Executor, nodeID types.NodeID) (bool, error) {
	rows, err := db.Exec(`
		SELECT 1 FROM malfeasance
		WHERE pubkey = ?1;`,
		func(stmt *sql.Statement) {
			stmt.BindBytes(1, nodeID.Bytes())
		}, nil,
	)
	if err != nil {
		return false, fmt.Errorf("is malicious %s: %w", nodeID, err)
	}
	return rows > 0, nil
}

func Proof(db sql.Executor, nodeID types.NodeID) ([]byte, error) {
	var proof []byte
	_, err := db.Exec(`
		SELECT proof FROM malfeasance
		WHERE pubkey = ?1;`,
		func(stmt *sql.Statement) {
			stmt.BindBytes(1, nodeID.Bytes())
		}, func(stmt *sql.Statement) bool {
			proof = make([]byte, stmt.ColumnLen(0))
			stmt.ColumnBytes(0, proof)
			return true
		},
	)
	if err != nil {
		return nil, fmt.Errorf("proof %v: %w", nodeID, err)
	}
	if proof != nil {
		return proof, nil
	}

	_, err = db.Exec(`
		SELECT proof FROM malfeasance
		WHERE pubkey = (
			SELECT married_to FROM malfeasance
			WHERE pubkey = ?1
		);`,
		func(stmt *sql.Statement) {
			stmt.BindBytes(1, nodeID.Bytes())
		}, func(stmt *sql.Statement) bool {
			proof = make([]byte, stmt.ColumnLen(0))
			stmt.ColumnBytes(0, proof)
			return true
		},
	)
	if err != nil {
		return nil, fmt.Errorf("proof %v: %w", nodeID, err)
	}

	return proof, nil
}

// TODO(mafa): it seems that this is again needed by the fetcher.
//
// The problem here is that iterate will iterate over all identities known to be malfeasant,
// independent of their marriage set. I believe we have to stick with this behavior,
// since we might have a different view on the marriage set than our peers, so we have to include
// ALL known malicious identities.
func Iterate(db sql.Executor, callback func(total int, id types.NodeID) error) error {
	var callbackErr error
	dec := func(stmt *sql.Statement) bool {
		var id types.NodeID
		total := stmt.ColumnInt(0)
		stmt.ColumnBytes(1, id[:])
		if err := callback(total, id); err != nil {
			return false
		}
		return true
	}

	_, err := db.Exec(`
		SELECT (SELECT count(*) FROM malfeasance) as total,
		pubkey FROM malfeasance;`,
		nil, dec,
	)
	if err != nil {
		return fmt.Errorf("iterate malfeasance: %w", err)
	}
	return callbackErr
}

// All retrieves all malicious node IDs from the database.
func All(db sql.Executor) ([]types.NodeID, error) {
	var nodeIDs []types.NodeID
	err := Iterate(db, func(total int, id types.NodeID) error {
		if nodeIDs == nil {
			nodeIDs = make([]types.NodeID, 0, total)
		}
		nodeIDs = append(nodeIDs, id)
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(nodeIDs) != cap(nodeIDs) {
		panic("BUG: bad malicious node ID count")
	}
	return nodeIDs, nil
}

// TODO(mafa): it looks like the fetcher needs this function?
// Implementing this is not trivial, as the blob size depends on how many identities are in the marriage set
// and the encoded proof might be for a different identity then requested.
//
// This query could be significantly slower than other "GetBlobSizes" queries.
func BlobSizes(db sql.Executor, ids [][]byte) (sizes []int, err error) {
	panic("implement me")
}

// TODO(mafa): it looks like the fetcher needs this function?
//
// Same as above - loading the blob from DB is not trivial, since it requires re-encoding a
// possibly different identity's proof with certificates from the current knowledge about the marriage set.
func LoadBlob(ctx context.Context, db sql.Executor, nodeID []byte, blob *sql.Blob) error {
	panic("implement me")
}
