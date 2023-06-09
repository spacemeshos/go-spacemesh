package blocks

import (
	"errors"
	"fmt"
	"io"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
)

const (
	valid   = 1
	invalid = -1
)

var ErrValidityNotDecided = errors.New("block validity undecided")

func decodeBlock(reader io.Reader, id types.BlockID) (*types.Block, error) {
	inner := types.InnerBlock{}
	_, err := codec.DecodeFrom(reader, &inner)
	if err != nil {
		return nil, fmt.Errorf("failed to decode block %s: %w", id, err)
	}
	return types.NewExistingBlock(id, inner), nil
}

// Add block to the database.
func Add(db sql.Executor, block *types.Block) error {
	bytes, err := codec.Encode(&block.InnerBlock)
	if err != nil {
		return fmt.Errorf("encode %w", err)
	}
	if _, err := db.Exec("insert into blocks (id, layer, block) values (?1, ?2, ?3);",
		func(stmt *sql.Statement) {
			stmt.BindBytes(1, block.ID().Bytes())
			stmt.BindInt64(2, int64(block.LayerIndex))
			stmt.BindBytes(3, bytes) // this is actually should encode block
		}, nil); err != nil {
		return fmt.Errorf("insert %s: %w", block.ID(), err)
	}
	return nil
}

// Has a block in the database.
func Has(db sql.Executor, id types.BlockID) (bool, error) {
	rows, err := db.Exec("select 1 from blocks where id = ?1;",
		func(stmt *sql.Statement) {
			stmt.BindBytes(1, id.Bytes())
		}, nil,
	)
	if err != nil {
		return false, fmt.Errorf("has ballot %s: %w", id, err)
	}
	return rows > 0, nil
}

// Get block with id from database.
func Get(db sql.Executor, id types.BlockID) (rst *types.Block, err error) {
	if rows, err := db.Exec("select block from blocks where id = ?1;", func(stmt *sql.Statement) {
		stmt.BindBytes(1, id.Bytes())
	}, func(stmt *sql.Statement) bool {
		rst, err = decodeBlock(stmt.ColumnReader(0), id)
		return true
	}); err != nil {
		return nil, fmt.Errorf("get block %s: %w", id, err)
	} else if rows == 0 {
		return nil, fmt.Errorf("get block %s: %w", id, sql.ErrNotFound)
	}
	return rst, err
}

func UpdateValid(db sql.Executor, id types.BlockID, valid bool) error {
	if valid {
		return SetValid(db, id)
	}
	return SetInvalid(db, id)
}

// SetValid updates verified status for a block.
func SetValid(db sql.Executor, id types.BlockID) error {
	return setValidity(db, id, valid)
}

// SetInvalid updates blocks to an invalid status.
func SetInvalid(db sql.Executor, id types.BlockID) error {
	return setValidity(db, id, invalid)
}

func setValidity(db sql.Executor, id types.BlockID, validity int8) error {
	if rows, err := db.Exec(`update blocks set validity=?2 where id = ?1 returning id;`, func(stmt *sql.Statement) {
		stmt.BindBytes(1, id.Bytes())
		stmt.BindInt64(2, int64(validity))
	}, nil); err != nil {
		return fmt.Errorf("update validity %s: %w", id, err)
	} else if rows == 0 {
		return fmt.Errorf("%w block for update %s", sql.ErrNotFound, id)
	}
	return nil
}

// IsValid returns true if block is verified.
func IsValid(db sql.Executor, id types.BlockID) (rst bool, err error) {
	if rows, err := db.Exec("select validity from blocks where id = ?1;", func(stmt *sql.Statement) {
		stmt.BindBytes(1, id.Bytes())
	}, func(stmt *sql.Statement) bool {
		if stmt.ColumnInt(0) == 0 {
			err = fmt.Errorf("%w: %s", ErrValidityNotDecided, id)
			return false
		}
		rst = stmt.ColumnInt(0) == valid
		return true
	}); err != nil {
		return false, fmt.Errorf("select verified %s: %w", id, err)
	} else if rows == 0 {
		return false, fmt.Errorf("%w block %s is not in the database", sql.ErrNotFound, id)
	}
	return rst, err
}

// GetLayer returns the layer of a block.
func GetLayer(db sql.Executor, id types.BlockID) (types.LayerID, error) {
	var lid types.LayerID
	if rows, err := db.Exec("select layer from blocks where id = ?1;", func(stmt *sql.Statement) {
		stmt.BindBytes(1, id.Bytes())
	}, func(stmt *sql.Statement) bool {
		lid = types.LayerID(uint32(stmt.ColumnInt64(0)))
		return true
	}); err != nil {
		return lid, fmt.Errorf("get block layer %s: %w", id, err)
	} else if rows == 0 {
		return lid, fmt.Errorf("%w block %s not in db", sql.ErrNotFound, err)
	}
	return lid, nil
}

// Layer returns full body blocks for layer.
func Layer(db sql.Executor, lid types.LayerID) ([]*types.Block, error) {
	var (
		blk *types.Block
		rst []*types.Block
		err error
	)
	if _, err = db.Exec("select id, block from blocks where layer = ?1;", func(stmt *sql.Statement) {
		stmt.BindInt64(1, int64(lid.Uint32()))
	}, func(stmt *sql.Statement) bool {
		id := types.BlockID{}
		stmt.ColumnBytes(0, id[:])
		blk, err = decodeBlock(stmt.ColumnReader(1), id)
		rst = append(rst, blk)
		return true
	}); err != nil {
		return nil, fmt.Errorf("select blocks in layer %s: %w", lid, err)
	}
	return rst, nil
}

// IDsInLayer returns list of block ids in the layer.
func IDsInLayer(db sql.Executor, lid types.LayerID) ([]types.BlockID, error) {
	var rst []types.BlockID
	if _, err := db.Exec("select id from blocks where layer = ?1;", func(stmt *sql.Statement) {
		stmt.BindInt64(1, int64(lid.Uint32()))
	}, func(stmt *sql.Statement) bool {
		id := types.BlockID{}
		stmt.ColumnBytes(0, id[:])
		rst = append(rst, id)
		return true
	}); err != nil {
		return nil, fmt.Errorf("select bids in layer %s: %w", lid, err)
	}
	return rst, nil
}

// ContextualValidity returns tuples with block id and contextual validity for all blocks in the layer.
func ContextualValidity(db sql.Executor, lid types.LayerID) ([]types.BlockContextualValidity, error) {
	var rst []types.BlockContextualValidity
	if _, err := db.Exec("select id, validity from blocks where layer = ?1;", func(stmt *sql.Statement) {
		stmt.BindInt64(1, int64(lid.Uint32()))
	}, func(stmt *sql.Statement) bool {
		validity := types.BlockContextualValidity{}
		stmt.ColumnBytes(0, validity.ID[:])
		validity.Validity = stmt.ColumnInt(1) == valid
		rst = append(rst, validity)
		return true
	}); err != nil {
		return nil, fmt.Errorf("contextual validity in layer %s: %w", lid, err)
	}
	return rst, nil
}
