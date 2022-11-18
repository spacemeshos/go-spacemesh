package transactions

import (
	"fmt"
	"time"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/sql"
)

const (
	stateApplied   = 1
	statePending   = 0
	stateDiscarded = -1
)

// Add transaction to the database or update the header if it wasn't set originally.
func Add(db sql.Executor, tx *types.Transaction, received time.Time) error {
	var (
		header []byte
		err    error
	)
	if tx.TxHeader != nil {
		header, err = codec.Encode(tx.TxHeader)
		if err != nil {
			return fmt.Errorf("encode %+v: %w", tx, err)
		}
	}
	if _, err = db.Exec(`
		insert into transactions (id, tx, header, layer, block, principal, nonce, timestamp, applied)
		values (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8, ?9)
		on conflict(id) do update set 
		header=?3, principal=?6, nonce=?7 
		where header is null;`,
		func(stmt *sql.Statement) {
			stmt.BindBytes(1, tx.ID.Bytes())
			stmt.BindBytes(2, tx.Raw)
			stmt.BindInt64(4, int64(types.LayerID{}.Value))
			stmt.BindBytes(5, types.EmptyBlockID.Bytes())

			if header != nil {
				stmt.BindBytes(3, header)
				stmt.BindBytes(6, tx.Principal[:])
				stmt.BindBytes(7, util.Uint64ToBytesBigEndian(tx.Nonce.Counter))
			}

			stmt.BindInt64(8, received.UnixNano())
			stmt.BindInt64(9, statePending)
		}, nil); err != nil {
		return fmt.Errorf("insert %s: %w", tx.ID, err)
	}
	return nil
}

// AddToProposal associates a transaction with a proposal.
func AddToProposal(db sql.Executor, tid types.TransactionID, lid types.LayerID, pid types.ProposalID) error {
	if _, err := db.Exec(`
		insert into proposal_transactions (pid, tid, layer) values (?1, ?2, ?3) 
		on conflict(tid, pid) do nothing;`,
		func(stmt *sql.Statement) {
			stmt.BindBytes(1, pid.Bytes())
			stmt.BindBytes(2, tid.Bytes())
			stmt.BindInt64(3, int64(lid.Value))
		}, nil); err != nil {
		return fmt.Errorf("add to proposal %s: %w", tid, err)
	}
	return nil
}

// HasProposalTX returns true if the given transaction is included in the given proposal.
func HasProposalTX(db sql.Executor, pid types.ProposalID, tid types.TransactionID) (bool, error) {
	rows, err := db.Exec("select 1 from proposal_transactions where pid = ?1 and tid = ?2",
		func(stmt *sql.Statement) {
			stmt.BindBytes(1, pid.Bytes())
			stmt.BindBytes(2, tid.Bytes())
		}, nil)
	if err != nil {
		return false, fmt.Errorf("has proposal txs %s/%s: %w", pid, tid, err)
	}
	return rows > 0, nil
}

// AddToBlock associates a transaction with a block.
func AddToBlock(db sql.Executor, tid types.TransactionID, lid types.LayerID, bid types.BlockID) error {
	if _, err := db.Exec(`
		insert into block_transactions (bid, tid, layer) values (?1, ?2, ?3)
		on conflict(tid, bid) do nothing;`,
		func(stmt *sql.Statement) {
			stmt.BindBytes(1, bid.Bytes())
			stmt.BindBytes(2, tid.Bytes())
			stmt.BindInt64(3, int64(lid.Value))
		}, nil); err != nil {
		return fmt.Errorf("add to block %s: %w", tid, err)
	}
	return nil
}

// HasBlockTX returns true if the given transaction is included in the given block.
func HasBlockTX(db sql.Executor, bid types.BlockID, tid types.TransactionID) (bool, error) {
	rows, err := db.Exec("select 1 from block_transactions where bid = ?1 and tid = ?2",
		func(stmt *sql.Statement) {
			stmt.BindBytes(1, bid.Bytes())
			stmt.BindBytes(2, tid.Bytes())
		}, nil)
	if err != nil {
		return false, fmt.Errorf("has block txs %s/%s: %w", bid, tid, err)
	}
	return rows > 0, nil
}

// GetAppliedLayer returns layer when transaction was applied.
func GetAppliedLayer(db sql.Executor, tid types.TransactionID) (types.LayerID, error) {
	var rst types.LayerID
	rows, err := db.Exec("select layer from transactions where id = ?1 and applied = ?2", func(stmt *sql.Statement) {
		stmt.BindBytes(1, tid[:])
		stmt.BindInt64(2, stateApplied)
	}, func(stmt *sql.Statement) bool {
		rst = types.NewLayerID(uint32(stmt.ColumnInt64(0)))
		return false
	})
	if err != nil {
		return types.LayerID{}, fmt.Errorf("failed to load applied layer for tx %s: %w", tid, err)
	}
	if rows == 0 {
		return types.LayerID{}, fmt.Errorf("%w: tx %s is not applied", sql.ErrNotFound, tid)
	}
	return rst, nil
}

// UndoLayers unset all transactions to `statePending` from `from` layer to the max layer with applied transactions.
func UndoLayers(db *sql.Tx, from types.LayerID) ([]types.TransactionID, error) {
	_, err := db.Exec(`delete from transactions_results_addresses 
		where tid in (select id from transactions where layer >= ?1);`,
		func(stmt *sql.Statement) {
			stmt.BindInt64(1, int64(from.Value))
		}, nil)
	if err != nil {
		return nil, fmt.Errorf("delete addresses mapping %w", err)
	}
	var updated []types.TransactionID
	_, err = db.Exec(`
			update transactions set applied = ?2, layer = ?3, block = ?4, result = null where layer >= ?1 returning id`,
		func(stmt *sql.Statement) {
			stmt.BindInt64(1, int64(from.Value))
			stmt.BindInt64(2, statePending)
			stmt.BindInt64(3, int64(types.LayerID{}.Value))
			stmt.BindBytes(4, types.EmptyBlockID.Bytes())
		}, func(stmt *sql.Statement) bool {
			var tid types.TransactionID
			stmt.ColumnBytes(0, tid[:])
			updated = append(updated, tid)
			return true
		})
	if err != nil {
		return nil, fmt.Errorf("undo layer %s: %w", from, err)
	}
	return updated, nil
}

// DiscardNonceBelow sets the applied field to `stateDiscarded` for transactions with nonce lower than specified.
func DiscardNonceBelow(db sql.Executor, address types.Address, nonce uint64) error {
	_, err := db.Exec(`update transactions set applied = ?3, layer = ?4, block = ?5 where principal = ?1 and nonce < ?2 and applied != ?6`,
		func(stmt *sql.Statement) {
			stmt.BindBytes(1, address.Bytes())
			stmt.BindBytes(2, util.Uint64ToBytesBigEndian(nonce))
			stmt.BindInt64(3, stateDiscarded)
			stmt.BindInt64(4, int64(types.LayerID{}.Value))
			stmt.BindBytes(5, types.EmptyBlockID.Bytes())
			stmt.BindInt64(6, stateApplied)
		}, nil)
	if err != nil {
		return fmt.Errorf("discard nonce below %s/%d: %w", address, nonce, err)
	}
	return nil
}

// DiscardByAcctNonce sets the applied field to `stateDiscarded` and layer to `lid` for transactions with addr and nonce.
func DiscardByAcctNonce(db sql.Executor, applied types.TransactionID, lid types.LayerID, addr types.Address, nonce uint64) error {
	_, err := db.Exec(`update transactions set applied = ?4, layer = ?5, block = ?6 where principal = ?1 and nonce = ?2 and id != ?3`,
		func(stmt *sql.Statement) {
			stmt.BindBytes(1, addr.Bytes())
			stmt.BindBytes(2, util.Uint64ToBytesBigEndian(nonce))
			stmt.BindBytes(3, applied.Bytes())
			stmt.BindInt64(4, stateDiscarded)
			stmt.BindInt64(5, int64(lid.Value))
			stmt.BindBytes(6, types.EmptyBlockID.Bytes())
		}, nil)
	if err != nil {
		return fmt.Errorf("discard %s/%d: %w", addr, nonce, err)
	}
	return nil
}

// tx, header, layer, block, timestamp.
func decodeTransaction(id types.TransactionID, applied int, stmt *sql.Statement) (*types.MeshTransaction, error) {
	var (
		parsed types.Transaction
		bid    types.BlockID
	)
	parsed.Raw = make([]byte, stmt.ColumnLen(0))
	stmt.ColumnBytes(0, parsed.Raw)
	if stmt.ColumnLen(1) > 0 {
		parsed.TxHeader = &types.TxHeader{}
		if _, err := codec.DecodeFrom(stmt.ColumnReader(1), parsed.TxHeader); err != nil {
			return nil, fmt.Errorf("decode %w", err)
		}
	}

	lid := types.NewLayerID(uint32(stmt.ColumnInt64(2)))
	stmt.ColumnBytes(3, bid[:])
	parsed.ID = id

	state := types.APPLIED
	switch applied {
	case stateApplied:
	case statePending:
		if lid == (types.LayerID{}) {
			state = types.MEMPOOL
		} else if bid == types.EmptyBlockID {
			state = types.PROPOSAL
		} else {
			state = types.BLOCK
		}
	case stateDiscarded:
		state = types.DISCARDED
	}
	return &types.MeshTransaction{
		Transaction: parsed,
		LayerID:     lid,
		BlockID:     bid,
		Received:    time.Unix(0, stmt.ColumnInt64(4)),
		State:       state,
	}, nil
}

// Get gets a transaction from database.
func Get(db sql.Executor, id types.TransactionID) (tx *types.MeshTransaction, err error) {
	var rows int
	rows, err = db.Exec("select tx, header, layer, block, timestamp, applied from transactions where id = ?1",
		func(stmt *sql.Statement) {
			stmt.BindBytes(1, id.Bytes())
		}, func(stmt *sql.Statement) bool {
			applied := stmt.ColumnInt(5)
			tx, err = decodeTransaction(id, applied, stmt)
			return err == nil
		})
	if err != nil {
		return nil, fmt.Errorf("get %s: %w", id, err)
	} else if rows == 0 {
		return nil, fmt.Errorf("%w: tx %s", sql.ErrNotFound, id)
	}
	return tx, nil
}

// GetBlob loads transaction as an encoded blob, ready to be sent over the wire.
func GetBlob(db sql.Executor, id []byte) (buf []byte, err error) {
	if rows, err := db.Exec("select tx from transactions where id = ?1",
		func(stmt *sql.Statement) {
			stmt.BindBytes(1, id)
		}, func(stmt *sql.Statement) bool {
			buf = make([]byte, stmt.ColumnLen(0))
			stmt.ColumnBytes(0, buf)
			return true
		}); err != nil {
		return nil, fmt.Errorf("get blob %s: %w", types.BytesToHash(id), err)
	} else if rows == 0 {
		return nil, fmt.Errorf("%w: tx %s", sql.ErrNotFound, types.BytesToHash(id))
	}
	return buf, nil
}

// Has returns true if transaction is stored in the database.
func Has(db sql.Executor, id types.TransactionID) (bool, error) {
	rows, err := db.Exec("select 1 from transactions where id = ?1",
		func(stmt *sql.Statement) {
			stmt.BindBytes(1, id.Bytes())
		}, nil)
	if err != nil {
		return false, fmt.Errorf("has %s: %w", id, err)
	}
	return rows > 0, nil
}

// GetByAddress finds all transactions for an address.
func GetByAddress(db sql.Executor, from, to types.LayerID, address types.Address) ([]*types.MeshTransaction, error) {
	var txs []*types.MeshTransaction
	if _, err := db.Exec(`
		select tx, header, layer, block, timestamp, applied, id from transactions
		where principal = ?1 and layer between ?2 and ?3 and applied != ?4`,
		func(stmt *sql.Statement) {
			stmt.BindBytes(1, address[:])
			stmt.BindInt64(2, int64(from.Value))
			stmt.BindInt64(3, int64(to.Value))
			stmt.BindInt64(4, stateDiscarded)
		}, func(stmt *sql.Statement) bool {
			var (
				tx      *types.MeshTransaction
				id      types.TransactionID
				applied int
			)
			applied = stmt.ColumnInt(5)
			stmt.ColumnBytes(6, id[:])
			tx, err := decodeTransaction(id, applied, stmt)
			if err != nil {
				return false
			}
			txs = append(txs, tx)
			return true
		}); err != nil {
		return nil, fmt.Errorf("get by addr %s: %w", address, err)
	}
	return txs, nil
}

// GetAllPending get all transactions that are not yet applied.
func GetAllPending(db sql.Executor) ([]*types.MeshTransaction, error) {
	return queryPending(db, `
		select tx, header, layer, block, timestamp, id from transactions
		where applied = ?1 and header is not null order by timestamp asc`,
		func(stmt *sql.Statement) {
			stmt.BindInt64(1, statePending)
		}, "get all pending")
}

// GetAcctPendingFromNonce get all pending transactions with nonce <= `from` for the given address.
func GetAcctPendingFromNonce(db sql.Executor, address types.Address, from uint64) ([]*types.MeshTransaction, error) {
	return queryPending(db, `
		select tx, header, layer, block, timestamp, id from transactions
		where applied = ?1 and principal = ?2 and nonce >= ?3 order by nonce asc`,
		func(stmt *sql.Statement) {
			stmt.BindInt64(1, statePending)
			stmt.BindBytes(2, address.Bytes())
			stmt.BindBytes(3, util.Uint64ToBytesBigEndian(from))
		}, "get acct pending from nonce")
}

// query MUST ensure that this order of fields tx, layer, block, principal, timestamp, id.
func queryPending(db sql.Executor, query string, encoder func(*sql.Statement), errStr string) (rst []*types.MeshTransaction, err error) {
	if _, err = db.Exec(query, encoder, func(stmt *sql.Statement) bool {
		var (
			tx *types.MeshTransaction
			id types.TransactionID
		)
		stmt.ColumnBytes(5, id[:])
		tx, err = decodeTransaction(id, statePending, stmt)
		if err != nil {
			return false
		}
		rst = append(rst, tx)
		return true
	}); err != nil {
		return nil, fmt.Errorf("%s: %w", errStr, err)
	}
	return rst, err
}

// AddResult adds result for the transaction.
func AddResult(db *sql.Tx, id types.TransactionID, rst *types.TransactionResult) error {
	buf, err := codec.Encode(rst)
	if err != nil {
		return fmt.Errorf("encode %w", err)
	}

	if rows, err := db.Exec(`update transactions
		set result = ?2, applied = ?3, layer = ?4, block = ?5 
		where id = ?1 and applied != ?3 returning id;`,
		func(stmt *sql.Statement) {
			stmt.BindBytes(1, id[:])
			stmt.BindBytes(2, buf)
			stmt.BindInt64(3, stateApplied)
			stmt.BindInt64(4, int64(rst.Layer.Value))
			stmt.BindBytes(5, rst.Block[:])
		},
		func(stmt *sql.Statement) bool {
			return false
		},
	); err != nil {
		return fmt.Errorf("insert result for %s: %w", id, err)
	} else if rows == 0 {
		return fmt.Errorf("invalid state for %s", id)
	}
	for i := range rst.Addresses {
		if _, err := db.Exec(`insert into transactions_results_addresses 
		(address, tid) values (?1, ?2);`,
			func(stmt *sql.Statement) {
				stmt.BindBytes(1, rst.Addresses[i][:])
				stmt.BindBytes(2, id[:])
			}, nil); err != nil {
			return fmt.Errorf("add address %s to %s: %w",
				rst.Addresses[i].String(), id[:], err)
		}
	}
	return nil
}

func TransactionInProposal(db sql.Executor, id types.TransactionID, lid types.LayerID) (types.LayerID, error) {
	var rst types.LayerID
	rows, err := db.Exec("select min(layer) from proposal_transactions where tid = ?1 and layer > ?2",
		func(stmt *sql.Statement) {
			stmt.BindBytes(1, id.Bytes())
			stmt.BindInt64(2, int64(lid.Value))
		}, func(s *sql.Statement) bool {
			rst = types.NewLayerID(uint32(s.ColumnInt64(0)))
			return false
		})
	if rows == 0 {
		return rst, fmt.Errorf("%w no proposal after %s with tx %s", sql.ErrNotFound, lid, id)
	}
	if err != nil {
		return rst, fmt.Errorf("tx in proposal %s: %w", id, err)
	}
	return rst, nil
}

func TransactionInBlock(db sql.Executor, id types.TransactionID, lid types.LayerID) (types.BlockID, types.LayerID, error) {
	var (
		rst types.LayerID
		bid types.BlockID
	)
	rows, err := db.Exec("select min(layer), bid from block_transactions where tid = ?1 and layer > ?2",
		func(stmt *sql.Statement) {
			stmt.BindBytes(1, id.Bytes())
			stmt.BindInt64(2, int64(lid.Value))
		}, func(s *sql.Statement) bool {
			rst = types.NewLayerID(uint32(s.ColumnInt64(0)))
			s.ColumnBytes(1, bid[:])
			return false
		})
	if err != nil {
		return bid, rst, fmt.Errorf("tx in block %s: %w", id, err)
	}
	if rows == 0 {
		return bid, rst, fmt.Errorf("%w no block after %s with tx %s", sql.ErrNotFound, lid, id)
	}
	return bid, rst, nil
}
