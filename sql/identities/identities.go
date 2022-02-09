package identities

import (
	"fmt"

	"github.com/spacemeshos/go-spacemesh/sql"
)

// SetMalicious records identity as malicious.
func SetMalicious(db sql.Executor, pubkey []byte) error {
	_, err := db.Exec(`insert into identities (pubkey, malicious) 
	values (?1, 1) 
	on conflict do nothing;`,
		func(stmt *sql.Statement) {
			stmt.BindBytes(1, pubkey)
		}, nil,
	)
	if err != nil {
		return fmt.Errorf("set malicious 0x%x: %w", pubkey, err)
	}
	return nil
}

// IsMalicious returns true if identity is known to be malicious.
func IsMalicious(db sql.Executor, pubkey []byte) (bool, error) {
	rows, err := db.Exec("select 1 from identities where pubkey = ?1;",
		func(stmt *sql.Statement) {
			stmt.BindBytes(1, pubkey)
		}, nil)
	if err != nil {
		return false, fmt.Errorf("is malicious 0x%x: %w", pubkey, err)
	}
	return rows > 0, nil
}
