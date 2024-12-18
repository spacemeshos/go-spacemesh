package malfeasance

import (
	"fmt"
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/builder"
	"github.com/spacemeshos/go-spacemesh/sql/marriage"
)

func AddProof(
	db sql.Executor,
	nodeID types.NodeID,
	marriageID *marriage.ID,
	proof []byte,
	domain byte,
	received time.Time,
) error {
	_, err := db.Exec(`
		INSERT INTO malfeasance (pubkey, marriage_id, proof, domain, received)
		VALUES (?1, ?2, ?3, ?4, ?5)
		ON CONFLICT(pubkey) DO NOTHING
	`, func(stmt *sql.Statement) {
		stmt.BindBytes(1, nodeID.Bytes())
		if marriageID != nil {
			stmt.BindInt64(2, int64(*marriageID))
		}
		if proof != nil {
			stmt.BindBytes(3, proof)
			stmt.BindInt64(4, int64(domain))
		}
		stmt.BindInt64(5, received.UnixNano())
	}, nil)
	if err != nil {
		return fmt.Errorf("add proof %v: %w", nodeID, err)
	}
	return nil
}

func SetMalicious(db sql.Executor, nodeID types.NodeID, marriageID marriage.ID, received time.Time) error {
	_, err := db.Exec(`
		INSERT INTO malfeasance (pubkey, marriage_id, received)
		VALUES (?1, ?2, ?3)
		ON CONFLICT(pubkey) DO UPDATE SET
			marriage_id = ?2
	`, func(stmt *sql.Statement) {
		stmt.BindBytes(1, nodeID.Bytes())
		stmt.BindInt64(2, int64(marriageID))
		stmt.BindInt64(3, received.UnixNano())
	}, nil)
	if err != nil {
		return fmt.Errorf("set malicious %v: %w", nodeID, err)
	}
	return nil
}

func IsMalicious(db sql.Executor, nodeID types.NodeID) (bool, error) {
	rows, err := db.Exec(`
		SELECT 1
		FROM malfeasance
		WHERE pubkey = ?1
	`, func(stmt *sql.Statement) {
		stmt.BindBytes(1, nodeID.Bytes())
	}, nil)
	if err != nil {
		return false, fmt.Errorf("is malicious %v: %w", nodeID, err)
	}
	return rows > 0, nil
}

// Proof returns the malfeasance proof for the given node ID. Returns sql.ErrNotFound if no proof for the given node ID
// exists. To return a proof for a marriage set use MarriageProof instead.
func NodeIDProof(db sql.Executor, nodeID types.NodeID) (byte, []byte, error) {
	var (
		domain byte
		proof  []byte
	)
	rows, err := db.Exec(`
		SELECT proof, domain
		FROM malfeasance
		WHERE pubkey = ?1 AND marriage_id IS NULL
	`, func(stmt *sql.Statement) {
		stmt.BindBytes(1, nodeID.Bytes())
	}, func(stmt *sql.Statement) bool {
		proof = make([]byte, stmt.ColumnLen(0))
		stmt.ColumnBytes(0, proof)
		domain = byte(stmt.ColumnInt64(1))
		return false
	})
	if err != nil {
		return 0, nil, fmt.Errorf("proof %v: %w", nodeID, err)
	}
	if rows == 0 {
		return 0, nil, sql.ErrNotFound
	}
	return domain, proof, nil
}

// MarriageProof returns the malfeasance proof for the marriage set. Returns sql.ErrNotFound if no proof for the given
// marriage ID exists. To return a proof for a node ID use NodeIDProof instead.
func MarriageProof(db sql.Executor, marriageID marriage.ID) (byte, []byte, error) {
	var (
		domain byte
		proof  []byte
	)
	rows, err := db.Exec(`
		SELECT proof, domain
		FROM malfeasance
		WHERE marriage_id = ?1 AND proof IS NOT NULL
	`, func(stmt *sql.Statement) {
		stmt.BindInt64(1, int64(marriageID))
	}, func(stmt *sql.Statement) bool {
		proof = make([]byte, stmt.ColumnLen(0))
		stmt.ColumnBytes(0, proof)
		domain = byte(stmt.ColumnInt64(1))
		return false
	})
	if err != nil {
		return 0, nil, fmt.Errorf("marriage proof %v: %w", marriageID, err)
	}
	if rows == 0 {
		return 0, nil, sql.ErrNotFound
	}
	return domain, proof, nil
}

func IterateOps(
	db sql.Executor,
	operations builder.Operations,
	fn func(types.NodeID, []byte, byte, time.Time) bool,
) error {
	fullQuery := `
		SELECT pubkey, proof, domain, received
		FROM malfeasance
	` + builder.FilterFrom(operations)
	_, err := db.Exec(fullQuery, builder.BindingsFrom(operations),
		func(stmt *sql.Statement) bool {
			var id types.NodeID
			stmt.ColumnBytes(0, id[:])
			var proof []byte
			if stmt.ColumnLen(1) > 0 {
				proof = make([]byte, stmt.ColumnLen(1))
				stmt.ColumnBytes(1, proof)
			}
			domain := byte(stmt.ColumnInt64(2))
			received := time.Unix(0, stmt.ColumnInt64(3))
			return fn(id, proof, domain, received)
		},
	)
	return err
}
