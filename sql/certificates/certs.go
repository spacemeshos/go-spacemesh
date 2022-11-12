package certificates

import (
	"fmt"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
)

func SetHareOutput(db sql.Executor, lid types.LayerID, bid types.BlockID) error {
	if _, err := db.Exec(`insert into certificates (layer, block, valid) values (?1, ?2, 1)
		on conflict do nothing;`,
		func(stmt *sql.Statement) {
			stmt.BindInt64(1, int64(lid.Value))
			stmt.BindBytes(2, bid[:])
		}, nil); err != nil {
		return fmt.Errorf("add wo cert %s: %w", lid, err)
	}
	return nil
}

// GetHareOutput returns the block that's valid as hare output for the specified layer.
// if there are more than one valid blocks, return types.EmptyBlockID.
func GetHareOutput(db sql.Executor, lid types.LayerID) (types.BlockID, error) {
	var (
		result types.BlockID
		err    error
		rows   int
	)
	if rows, err = db.Exec("select block from certificates where layer = ?1 and valid = 1;", func(stmt *sql.Statement) {
		stmt.BindInt64(1, int64(lid.Value))
	}, func(stmt *sql.Statement) bool {
		stmt.ColumnBytes(0, result[:])
		return true
	}); err != nil {
		return types.EmptyBlockID, fmt.Errorf("get certs %s: %w", lid, err)
	} else if rows == 0 {
		return types.EmptyBlockID, fmt.Errorf("get certs %s: %w", lid, sql.ErrNotFound)
	} else if rows > 1 {
		return types.EmptyBlockID, nil
	}
	return result, nil
}

func Add(db sql.Executor, lid types.LayerID, cert *types.Certificate) error {
	data, err := codec.Encode(cert)
	if err != nil {
		return fmt.Errorf("encode cert %w", err)
	}
	if _, err = db.Exec(`insert into certificates (layer, block, cert, valid) values (?1, ?2, ?3, 1)
		on conflict do update set cert = ?3, valid = 1;`,
		func(stmt *sql.Statement) {
			stmt.BindInt64(1, int64(lid.Value))
			stmt.BindBytes(2, cert.BlockID[:])
			stmt.BindBytes(3, data[:])
		}, nil); err != nil {
		return fmt.Errorf("add cert %s: %w", lid, err)
	}
	return nil
}

type CertValidity struct {
	Block types.BlockID
	Cert  *types.Certificate
	Valid bool
}

func Get(db sql.Executor, lid types.LayerID) ([]CertValidity, error) {
	var result []CertValidity
	if rows, err := db.Exec("select block, cert, valid from certificates where layer = ?1 order by length(cert) desc;", func(stmt *sql.Statement) {
		stmt.BindInt64(1, int64(lid.Value))
	}, func(stmt *sql.Statement) bool {
		var (
			cv   CertValidity
			cert types.Certificate
		)
		stmt.ColumnBytes(0, cv.Block[:])
		if stmt.ColumnLen(1) > 0 {
			data := make([]byte, stmt.ColumnLen(1))
			stmt.ColumnBytes(1, data[:])
			if err := codec.Decode(data, &cert); err != nil {
				return false
			}
			cv.Cert = &cert
		}
		cv.Valid = stmt.ColumnInt(2) > 0
		result = append(result, cv)
		return true
	}); err != nil {
		return nil, fmt.Errorf("get certs %s: %w", lid, err)
	} else if rows == 0 {
		return nil, fmt.Errorf("get certs %s: %w", lid, sql.ErrNotFound)
	}
	return result, nil
}

func SetValid(db sql.Executor, lid types.LayerID, bid types.BlockID) error {
	if _, err := db.Exec(`update certificates set valid = 1 where layer = ?1 and block = ?2;`,
		func(stmt *sql.Statement) {
			stmt.BindInt64(1, int64(lid.Value))
			stmt.BindBytes(2, bid[:])
		}, nil); err != nil {
		return fmt.Errorf("invalidate %s: %w", lid, err)
	}
	return nil
}

func SetInvalid(db sql.Executor, lid types.LayerID, bid types.BlockID) error {
	if _, err := db.Exec(`update certificates set valid = 0 where layer = ?1 and block = ?2;`,
		func(stmt *sql.Statement) {
			stmt.BindInt64(1, int64(lid.Value))
			stmt.BindBytes(2, bid[:])
		}, nil); err != nil {
		return fmt.Errorf("invalidate %s: %w", lid, err)
	}
	return nil
}
