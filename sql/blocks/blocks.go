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
		return err
	}
	return db.Exec("insert or ignore into blocks (id, layer, block) values (?1, ?2, ?3);",
		func(stmt *sql.Statement) {
			stmt.BindBytes(1, block.ID().Bytes())
			stmt.BindInt64(2, int64(block.LayerIndex.Value))
			stmt.BindBytes(3, bytes) // this is actually should encode block
		}, nil)
}

// AddHareOutput records hare output.
func AddHareOutput(db sql.Executor, output types.BlockID) error {
	if err := db.Exec("update blocks set hare_output = 1 where id = ?1;", func(stmt *sql.Statement) {
		stmt.BindBytes(1, output.Bytes())
	}, nil); err != nil {
		return fmt.Errorf("update hare_output to %s: %w", output, err)
	}

	return nil
}

// UpdateVerified updates verified status for a block.
func UpdateVerified(db sql.Executor, id types.BlockID) error {
	if err := db.Exec("update blocks set verified = 1 where id = ?1;", func(stmt *sql.Statement) {
		stmt.BindBytes(1, id.Bytes())
	}, nil); err != nil {
		return fmt.Errorf("update verified %s: %w", id, err)
	}
	return nil
}

// AddVerified updates verified blocks.
func AddVerified(db sql.Executor, verified []types.BlockID) error {
	for _, id := range verified {
		if err := UpdateVerified(db, id); err != nil {
			return err
		}
	}
	return nil
}

// Get block with id from database.
func Get(db sql.Executor, id types.BlockID) (rst *types.Block, err error) {
	if err := db.Exec("select block from blocks where id = ?1;", func(stmt *sql.Statement) {
		stmt.BindBytes(1, id.Bytes())
	}, func(stmt *sql.Statement) bool {
		rst, err = decodeBlock(stmt.ColumnReader(0), id)
		return true
	}); err != nil {
		return nil, fmt.Errorf("get %s: %w", id, err)
	}
	return rst, err
}

// IsVerified returns true if block is verified.
func IsVerified(db sql.Executor, id types.BlockID) (bool, error) {
	var rst bool
	if err := db.Exec("select verified from blocks where id = ?1;", func(stmt *sql.Statement) {
		stmt.BindBytes(1, id.Bytes())
	}, func(stmt *sql.Statement) bool {
		rst = stmt.ColumnInt(0) != 0
		return true
	}); err != nil {
		return false, fmt.Errorf("select verified %s: %w", id, err)
	}
	return rst, nil
}

func InLayer(db sql.Executor, lid types.LayerID) ([]types.BlockID, error) {
	var rst []types.BlockID
	if err := db.Exec("select id from blocks where layer = ?1;", func(stmt *sql.Statement) {
		stmt.BindInt64(1, int64(lid.Uint32()))
	}, func(stmt *sql.Statement) bool {
		id := types.BlockID{}
		stmt.ColumnBytes(0, id[:])
		rst = append(rst, id)
		return true
	}); err != nil {
		return nil, err
	}
	return rst, nil
}

func ValidityInLayer(db sql.Executor, lid types.LayerID) (map[types.BlockID]bool, error) {
	rst := map[types.BlockID]bool{}
	if err := db.Exec("select (id, verified) from blocks where layer = ?1;", func(stmt *sql.Statement) {
		stmt.BindInt64(1, int64(lid.Uint32()))
	}, func(stmt *sql.Statement) bool {
		id := types.BlockID{}
		stmt.ColumnBytes(0, id[:])
		rst[id] = stmt.ColumnInt(1) == 1
		return true
	}); err != nil {
		return nil, err
	}
	return rst, nil
}

// GetHareOutput returns id of the block.
func GetHareOutput(db sql.Executor, lid types.LayerID) (types.BlockID, error) {
	var rst types.BlockID
	if err := db.Exec("select id from blocks where layer = ?1 and hare_output = 1;", func(stmt *sql.Statement) {
		stmt.BindInt64(1, int64(lid.Uint32()))
	}, func(stmt *sql.Statement) bool {
		id := types.BlockID{}
		stmt.ColumnBytes(0, id[:])
		return true
	}); err != nil {
		return types.BlockID{}, fmt.Errorf("select hare output %s: %w", lid, err)
	}
	return rst, nil
}

// GetVerified returns list of verified blocks in a layer.
func GetVerified(db sql.Executor, lid types.LayerID) ([]types.BlockID, error) {
	var rst []types.BlockID
	if err := db.Exec("select id from blocks where layer = ?1 and verified = 1;", func(stmt *sql.Statement) {
		stmt.BindInt64(1, int64(lid.Uint32()))
	}, func(stmt *sql.Statement) bool {
		id := types.BlockID{}
		stmt.ColumnBytes(0, id[:])
		rst = append(rst, id)
		return true
	}); err != nil {
		return nil, err
	}
	return rst, nil
}
