package certifiedblocks

import (
	"fmt"
	certtypes "github.com/spacemeshos/go-spacemesh/blockcerts/types"
	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
)

func Add(db sql.Executor, blockCert *certtypes.BlockCertificate) error {
    certBytes, err := codec.Encode(blockCert)
    if err != nil {
        return fmt.Errorf("encoding cert for block %s: %w",
            blockCert.BlockID.String(), err)
    }
    _, err = db.Exec(
        `insert into certified_blocks (block_id, layer_id, cert) 
		values (?1, ?2, ?3);`,
        func(statement *sql.Statement) {
            statement.BindBytes(1, blockCert.BlockID.Bytes())
            statement.BindBytes(2, blockCert.LayerID.Bytes())
            statement.BindBytes(3, certBytes)
        }, nil,
    )
    if err != nil {
        return fmt.Errorf("adding blockCert %s: %w",
            blockCert.BlockID.String(), err)
    }
    return nil
}

func Get(db sql.Executor, id types.BlockID,
) (cert *certtypes.BlockCertificate, err error) {
    if rows, err := db.Exec(
        "select cert from certified_blocks where block_id = ?1;",
        func(stmt *sql.Statement) {
            stmt.BindBytes(1, id.Bytes())
        },
        func(stmt *sql.Statement) bool {
            cert = &certtypes.BlockCertificate{}
            _, err = codec.DecodeFrom(stmt.GetReader("cert"), cert)
            return true
        }); err != nil {
        return nil, fmt.Errorf("get %s: %w", id, err)
    } else if rows == 0 {
        return nil, fmt.Errorf("%w block %s", sql.ErrNotFound, id)
    }
    return cert, nil
}
func Has(db sql.Executor, id types.BlockID) (bool, error) {
    rows, err := db.Exec("select 1 from certified_blocks where block_id = ?1;",
        func(stmt *sql.Statement) {
            stmt.BindBytes(1, id.Bytes())
        }, nil,
    )
    if err != nil {
        return false, fmt.Errorf("has certified block %s: %w", id, err)
    }
    return rows > 0, nil
}
