package accounts

import (
	"fmt"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
)

// Latest latest account data for an address.
func Latest(db sql.Executor, address types.Address) (types.Account, error) {
	var account types.Account
	_, err := db.Exec("select balance, nonce, layer_updated from accounts where address = ?1;", func(stmt *sql.Statement) {
		stmt.BindBytes(1, address.Bytes())
	}, func(stmt *sql.Statement) bool {
		account.Balance = uint64(stmt.ColumnInt64(0))
		account.Nonce = uint64(stmt.ColumnInt64(1))
		account.Layer = types.NewLayerID(uint32(stmt.ColumnInt64(2)))
		return false
	})
	if err != nil {
		return account, fmt.Errorf("failed to load %v: %w", address, err)
	}
	account.Address = address
	return account, nil
}

// All returns all latest accounts.
func All(db sql.Executor) ([]*types.Account, error) {
	var rst []*types.Account
	_, err := db.Exec("select address, nonce, balance, max(layer_updated) from accounts group by address;", nil, func(stmt *sql.Statement) bool {
		var account types.Account
		stmt.ColumnBytes(0, account.Address[:])
		account.Balance = uint64(stmt.ColumnInt64(1))
		account.Nonce = uint64(stmt.ColumnInt64(2))
		account.Layer = types.NewLayerID(uint32(stmt.ColumnInt64(3)))
		rst = append(rst, &account)
		return true
	})
	if err != nil {
		return nil, fmt.Errorf("failed to load all accounts %w", err)
	}
	return rst, nil
}

// Get account data that was valid at the specified layer.
func Get(db sql.Executor, address types.Address, layer types.LayerID) (types.Account, error) {
	// TODO dry it
	var account types.Account
	_, err := db.Exec("select balance, nonce, layer_updated from accounts where address = ?1 and layer_updated <= ?2;", func(stmt *sql.Statement) {
		stmt.BindBytes(1, address.Bytes())
		stmt.BindInt64(2, int64(layer.Value))
	}, func(stmt *sql.Statement) bool {
		account.Balance = uint64(stmt.ColumnInt64(0))
		account.Nonce = uint64(stmt.ColumnInt64(1))
		account.Layer = types.NewLayerID(uint32(stmt.ColumnInt64(2)))
		return false
	})
	if err != nil {
		return account, fmt.Errorf("failed to load %v for layer %v: %w", address, layer, err)
	}
	account.Address = address
	return account, nil
}

// Update account state at a certain layer.
func Update(db sql.Executor, to *types.Account) error {
	_, err := db.Exec(`insert into 
	accounts (address, balance, nonce, layer_updated) 
	values (?1, ?2, ?3, ?4);`, func(stmt *sql.Statement) {
		stmt.BindBytes(1, to.Address.Bytes())
		stmt.BindInt64(2, int64(to.Balance))
		stmt.BindInt64(3, int64(to.Nonce))
		stmt.BindInt64(4, int64(to.Layer.Value))
	}, nil)
	if err != nil {
		return fmt.Errorf("failed to insert account %s for layer %v: %w", to.Address, to.Layer, err)
	}
	return nil
}

// Revert state after the layer.
func Revert(db sql.Executor, after types.LayerID) error {
	_, err := db.Exec(`delete from accounts where layer_updated > ?1;`,
		func(stmt *sql.Statement) {
			stmt.BindInt64(1, int64(after.Value))
		}, nil)
	if err != nil {
		return fmt.Errorf("failed to revert up to %v: %w", after, err)
	}
	return nil
}
