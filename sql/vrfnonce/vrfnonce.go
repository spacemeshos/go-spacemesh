package vrfnonce

import (
	"fmt"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
)

// Get gets an ATX by a given ATX ID.
func Get(db sql.Executor, id types.NodeID, epoch types.EpochID) (nonce types.VRFPostIndex, err error) {
	enc := func(stmt *sql.Statement) {
		stmt.BindBytes(1, id.Bytes())
		stmt.BindInt64(2, int64(epoch))
	}
	dec := func(stmt *sql.Statement) bool {
		nonce = types.VRFPostIndex(stmt.ColumnInt64(0))
		return true
	}

	if rows, err := db.Exec(`
		select nonce from vrf_nonces
		where id = ?1 and epoch <= ?2
		order by epoch desc
		limit 1;`, enc, dec); err != nil {
		return types.VRFPostIndex(0), fmt.Errorf("exec id %v, epoch %d: %w", id, epoch, err)
	} else if rows == 0 {
		return types.VRFPostIndex(0), fmt.Errorf("exec id %v, epoch %d: %w", id, epoch, sql.ErrNotFound)
	}

	return nonce, err
}

// Add adds an ATX for a given ATX ID.
func Add(db sql.Executor, id types.NodeID, epoch types.EpochID, nonce types.VRFPostIndex) error {
	enc := func(stmt *sql.Statement) {
		stmt.BindBytes(1, id.Bytes())
		stmt.BindInt64(2, int64(epoch))
		stmt.BindInt64(3, int64(nonce))
	}

	_, err := db.Exec(`
		insert into vrf_nonces (id, epoch, nonce) 
		values (?1, ?2, ?3);`, enc, nil)
	if err != nil {
		return fmt.Errorf("insert nonce NodeID %v, epoch %d: %w", id, epoch, err)
	}
	return nil
}
