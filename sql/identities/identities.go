package identities

import "github.com/spacemeshos/go-spacemesh/sql"

// AddMalicious registers public key as malicious.
func AddMalicious(db sql.Executor, pubkey []byte, malicious bool) error {
	return nil
}
