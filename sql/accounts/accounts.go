package accounts

import (
	"fmt"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/sql"
)

func load(db sql.Executor, address types.Address, query string, enc sql.Encoder) (types.Account, error) {
	var account types.Account
	_, err := db.Exec(query, enc, func(stmt *sql.Statement) bool {
		account.Balance = uint64(stmt.ColumnInt64(0))
		account.NextNonce = uint64(stmt.ColumnInt64(1))
		account.Layer = types.LayerID(uint32(stmt.ColumnInt64(2)))
		if stmt.ColumnLen(3) > 0 {
			account.TemplateAddress = &types.Address{}
			stmt.ColumnBytes(3, account.TemplateAddress[:])
			account.State = make([]byte, stmt.ColumnLen(4))
			stmt.ColumnBytes(4, account.State)
		}
		return false
	})
	if err != nil {
		return types.Account{}, err
	}
	account.Address = address
	return account, nil
}

// Has the account in the database.
func Has(db sql.Executor, address types.Address) (bool, error) {
	rows, err := db.Exec("select 1 from accounts where address = ?1;",
		func(stmt *sql.Statement) {
			stmt.BindBytes(1, address.Bytes())
		}, nil,
	)
	if err != nil {
		return false, fmt.Errorf("has address %v: %w", address, err)
	}
	return rows > 0, nil
}

// Latest latest account data for an address.
func Latest(db sql.Executor, address types.Address) (types.Account, error) {
	account, err := load(db, address, "select balance, next_nonce, layer_updated, template, state from accounts where address = ?1;", func(stmt *sql.Statement) {
		stmt.BindBytes(1, address.Bytes())
	})
	if err != nil {
		return types.Account{}, fmt.Errorf("failed to load %v: %w", address, err)
	}
	return account, nil
}

// Get account data that was valid at the specified layer.
func Get(db sql.Executor, address types.Address, layer types.LayerID) (types.Account, error) {
	account, err := load(db, address, "select balance, next_nonce, layer_updated, template, state from accounts where address = ?1 and layer_updated <= ?2;", func(stmt *sql.Statement) {
		stmt.BindBytes(1, address.Bytes())
		stmt.BindInt64(2, int64(layer))
	})
	if err != nil {
		return types.Account{}, fmt.Errorf("failed to load %v for layer %v: %w", address, layer, err)
	}
	return account, nil
}

// All returns all latest accounts.
func All(db sql.Executor) ([]*types.Account, error) {
	var rst []*types.Account
	_, err := db.Exec("select address, balance, next_nonce, max(layer_updated), template, state from accounts group by address;", nil, func(stmt *sql.Statement) bool {
		var account types.Account
		stmt.ColumnBytes(0, account.Address[:])
		account.Balance = uint64(stmt.ColumnInt64(1))
		account.NextNonce = uint64(stmt.ColumnInt64(2))
		account.Layer = types.LayerID(uint32(stmt.ColumnInt64(3)))
		if stmt.ColumnLen(4) > 0 {
			var template types.Address
			stmt.ColumnBytes(4, template[:])
			account.TemplateAddress = &template
			account.State = make([]byte, stmt.ColumnLen(5))
			stmt.ColumnBytes(5, account.State)
		}
		rst = append(rst, &account)
		return true
	})
	if err != nil {
		return nil, fmt.Errorf("failed to load all accounts %w", err)
	}
	return rst, nil
}

func Snapshot(db sql.Executor, layer types.LayerID) ([]*types.Account, error) {
	var rst []*types.Account
	_, err := db.Exec(`
			select address, balance, next_nonce, max(layer_updated), template, state from accounts 
			where layer_updated <= ?1
			group by address order by address asc;`,
		func(stmt *sql.Statement) {
			stmt.BindInt64(1, int64(layer))
		},
		func(stmt *sql.Statement) bool {
			var account types.Account
			stmt.ColumnBytes(0, account.Address[:])
			account.Balance = uint64(stmt.ColumnInt64(1))
			account.NextNonce = uint64(stmt.ColumnInt64(2))
			account.Layer = types.LayerID(uint32(stmt.ColumnInt64(3)))
			if stmt.ColumnLen(4) > 0 {
				var template types.Address
				stmt.ColumnBytes(4, template[:])
				account.TemplateAddress = &template
				account.State = make([]byte, stmt.ColumnLen(5))
				stmt.ColumnBytes(5, account.State)
			}
			rst = append(rst, &account)
			return true
		})
	if err != nil {
		return nil, fmt.Errorf("failed to load all accounts %w", err)
	}
	return rst, nil
}

// Update account state at a certain layer.
func Update(db sql.Executor, to *types.Account) error {
	_, err := db.Exec(`insert into 
	accounts (address, balance, next_nonce, layer_updated, template, state) 
	values (?1, ?2, ?3, ?4, ?5, ?6);`, func(stmt *sql.Statement) {
		stmt.BindBytes(1, to.Address.Bytes())
		stmt.BindInt64(2, int64(to.Balance))
		stmt.BindInt64(3, int64(to.NextNonce))
		stmt.BindInt64(4, int64(to.Layer))
		if to.TemplateAddress == nil {
			stmt.BindNull(5)
			stmt.BindNull(6)
		} else {
			stmt.BindBytes(5, to.TemplateAddress[:])
			stmt.BindBytes(6, to.State)
		}
	}, nil)
	if err != nil {
		return fmt.Errorf("failed to insert account %v for layer %v: %w", to.Address.String(), to.Layer, err)
	}
	return nil
}

// Revert state after the layer.
func Revert(db sql.Executor, after types.LayerID) error {
	_, err := db.Exec(`delete from accounts where layer_updated > ?1;`,
		func(stmt *sql.Statement) {
			stmt.BindInt64(1, int64(after))
		}, nil)
	if err != nil {
		return fmt.Errorf("failed to revert up to %v: %w", after, err)
	}
	return nil
}
