package malfeasance

import (
	"fmt"
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/marriage"
)

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

func AddProof(db sql.Executor, nodeID types.NodeID, proof []byte, domain int, received time.Time) error {
	_, err := db.Exec(`
		INSERT INTO malfeasance (pubkey, proof, domain, received)
		VALUES (?1, ?2, ?3, ?4)
		ON CONFLICT DO NOTHING
	`, func(stmt *sql.Statement) {
		stmt.BindBytes(1, nodeID.Bytes())
		stmt.BindBytes(2, proof)
		stmt.BindInt64(3, int64(domain))
		stmt.BindInt64(4, received.UnixNano())
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
		ON CONFLICT DO NOTHING
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
