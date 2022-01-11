package blocks

import (
	"fmt"
	"io"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
)

func decodeBlock(reader io.Reader, id types.BlockID) (*types.Block, error) {
	inner := types.InnerBlock{}
	_, err := codec.DecodeFrom(reader, &inner)
	if err != nil {
		return nil, fmt.Errorf("failed to decode block %s: %w", id, err)
	}
	block := types.NewExistingBlock(id, inner)
	return &block, nil
}

// Add block to the database.
func Add(db sql.Executor, block *types.Block) error {
	bytes, err := codec.Encode(block.InnerBlock)
	if err != nil {
		return fmt.Errorf("encode %w", err)
	}
	if _, err := db.Exec("insert into blocks (id, layer, block) values (?1, ?2, ?3);",
		func(stmt *sql.Statement) {
			stmt.BindBytes(1, block.ID().Bytes())
			stmt.BindInt64(2, int64(block.LayerIndex.Value))
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
		return nil, fmt.Errorf("get %s: %w", id, err)
	} else if rows == 0 {
		return nil, fmt.Errorf("%w block %s", sql.ErrNotFound, id)
	}
	return rst, err
}

// SetVerified updates verified status for a block.
func SetVerified(db sql.Executor, id types.BlockID) error {
	if rows, err := db.Exec(`insert into blocks (id, verified) values (?1, ?2) 
	on conflict(id) do update set verified=?2 returning id;`, func(stmt *sql.Statement) {
		stmt.BindBytes(1, id.Bytes())
		stmt.BindInt64(2, 1)
	}, nil); err != nil {
		return fmt.Errorf("update verified %s: %w", id, err)
	} else if rows == 0 {
		return fmt.Errorf("%w block for update %s", sql.ErrNotFound, id)
	}
	return nil
}

// SetInvalid updates blocks to an invalid status.
func SetInvalid(db sql.Executor, id types.BlockID) error {
	if rows, err := db.Exec(`insert into blocks (id, verified) values (?1, ?2) 
	on conflict(id) do update set verified=?2 returning id;`, func(stmt *sql.Statement) {
		stmt.BindBytes(1, id.Bytes())
		stmt.BindInt64(2, -1)
	}, nil); err != nil {
		return fmt.Errorf("update invalid %s: %w", id, err)
	} else if rows == 0 {
		return fmt.Errorf("%w block for update %s", sql.ErrNotFound, id)
	}
	return nil
}

// IsVerified returns true if block is verified.
func IsVerified(db sql.Executor, id types.BlockID) (rst bool, err error) {
	if rows, err := db.Exec("select verified from blocks where id = ?1;", func(stmt *sql.Statement) {
		stmt.BindBytes(1, id.Bytes())
	}, func(stmt *sql.Statement) bool {
		if stmt.ColumnInt(0) == 0 {
			err = fmt.Errorf("%w block %s is undecided", sql.ErrNotFound, id)
			return false
		}
		rst = stmt.ColumnInt(0) == 1
		return true
	}); err != nil {
		return false, fmt.Errorf("select verified %s: %w", id, err)
	} else if rows == 0 {
		return false, fmt.Errorf("%w block %s is not in the database", sql.ErrNotFound, err)
	}
	return rst, err
}

// LayerIDs returns list of block ids in the layer.
func LayerIDs(db sql.Executor, lid types.LayerID) ([]types.BlockID, error) {
	var rst []types.BlockID
	if _, err := db.Exec("select id from blocks where layer = ?1;", func(stmt *sql.Statement) {
		stmt.BindInt64(1, int64(lid.Uint32()))
	}, func(stmt *sql.Statement) bool {
		id := types.BlockID{}
		stmt.ColumnBytes(0, id[:])
		rst = append(rst, id)
		return true
	}); err != nil {
		return nil, fmt.Errorf("select in layer %s: %w", lid, err)
	}
	return rst, nil
}
