package pending_txs

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/stretchr/testify/require"
	"testing"
)

func newTx(origin types.Address, nonce, totalAmount uint64) *types.Transaction {
	feeAmount := uint64(1)
	return types.NewTxWithOrigin(nonce, origin, types.Address{}, totalAmount-feeAmount, 3, feeAmount)
}

func TestNewAccountPendingTxs(t *testing.T) {
	r := require.New(t)

	pendingTxs := NewAccountPendingTxs()
	prevNonce := uint64(5)
	prevBalance := uint64(1000)

	// Empty list should project same as input
	empty := pendingTxs.IsEmpty()
	r.True(empty)
	nonce, balance := pendingTxs.GetProjection(prevNonce, prevBalance)
	r.Equal(prevNonce, nonce)
	r.Equal(prevBalance, balance)

	// Adding works
	pendingTxs.Add(1, newTx(types.BytesToAddress(nil), 5, 100))
	empty = pendingTxs.IsEmpty()
	r.False(empty)
	nonce, balance = pendingTxs.GetProjection(prevNonce, prevBalance)
	r.Equal(prevNonce+1, nonce)
	r.Equal(prevBalance-100, balance)

	// Accepting a transaction removes all same-nonce transactions
	pendingTxs.Remove([]*types.Transaction{
		newTx(types.BytesToAddress(nil), 5, 50),
	}, nil, 1)
	empty = pendingTxs.IsEmpty()
	r.True(empty)
	nonce, balance = pendingTxs.GetProjection(prevNonce, prevBalance)
	r.Equal(prevNonce, nonce)
	r.Equal(prevBalance, balance)

	// Add a transaction again
	pendingTxs.Add(1, newTx(types.BytesToAddress(nil), 5, 100))
	nonce, balance = pendingTxs.GetProjection(prevNonce, prevBalance)
	r.Equal(prevNonce+1, nonce)
	r.Equal(prevBalance-100, balance)

	// Now add it again in a higher layer -- this layer is now required when rejecting
	pendingTxs.Add(2, newTx(types.BytesToAddress(nil), 5, 100))
	nonce, balance = pendingTxs.GetProjection(prevNonce, prevBalance)
	r.Equal(prevNonce+1, nonce)
	r.Equal(prevBalance-100, balance)

	// When it's added again in a lower layer -- nothing changes
	pendingTxs.Add(0, newTx(types.BytesToAddress(nil), 5, 100))
	nonce, balance = pendingTxs.GetProjection(prevNonce, prevBalance)
	r.Equal(prevNonce+1, nonce)
	r.Equal(prevBalance-100, balance)

	// Rejecting a transaction with same-nonce does NOT remove a different transactions
	pendingTxs.Remove(nil, []*types.Transaction{
		newTx(types.BytesToAddress(nil), 5, 50),
	}, 2)
	nonce, balance = pendingTxs.GetProjection(prevNonce, prevBalance)
	r.Equal(prevNonce+1, nonce)
	r.Equal(prevBalance-100, balance)

	// Rejecting a transaction in a lower layer than the highest seen for it, does not remove it, either
	pendingTxs.Remove(nil, []*types.Transaction{
		newTx(types.BytesToAddress(nil), 5, 100),
	}, 1)
	nonce, balance = pendingTxs.GetProjection(prevNonce, prevBalance)
	r.Equal(prevNonce+1, nonce)
	r.Equal(prevBalance-100, balance)

	// However, rejecting a transaction in the highest layer it was seen in DOES remove it
	pendingTxs.Remove(nil, []*types.Transaction{
		newTx(types.BytesToAddress(nil), 5, 100),
	}, 2)
	nonce, balance = pendingTxs.GetProjection(prevNonce, prevBalance)
	r.Equal(prevNonce, nonce)
	r.Equal(prevBalance, balance)

	// When several transactions exist with same nonce, projection is pessimistic (reduces balance by higher amount)
	pendingTxs.Add(1,
		newTx(types.BytesToAddress(nil), 5, 100),
		newTx(types.BytesToAddress(nil), 5, 50),
	)
	nonce, balance = pendingTxs.GetProjection(prevNonce, prevBalance)
	r.Equal(prevNonce+1, nonce)
	r.Equal(prevBalance-100, balance)

	// Adding higher nonce transactions with a missing nonce in between does not affect projection
	pendingTxs.Add(1,
		newTx(types.BytesToAddress(nil), 7, 100),
		newTx(types.BytesToAddress(nil), 8, 50),
	)
	nonce, balance = pendingTxs.GetProjection(prevNonce, prevBalance)
	r.Equal(prevNonce+1, nonce)
	r.Equal(prevBalance-100, balance)

	// Trying to fill the gap with a transaction that would over-draft the account has no effect
	pendingTxs.Add(1, newTx(types.BytesToAddress(nil), 6, 950))
	nonce, balance = pendingTxs.GetProjection(prevNonce, prevBalance)
	r.Equal(prevNonce+1, nonce)
	r.Equal(prevBalance-100, balance)

	// But filling the gap with a valid transaction causes the higher nonce transactions to start being considered
	pendingTxs.Add(1, newTx(types.BytesToAddress(nil), 6, 50))
	nonce, balance = pendingTxs.GetProjection(prevNonce, prevBalance)
	r.Equal(prevNonce+4, nonce)
	r.Equal(prevBalance-100-50-100-50, balance)

	// Rejecting a transaction only removes that version, if several exist
	// This can also cause a transaction that would previously over-draft the account to become valid
	pendingTxs.Remove(nil, []*types.Transaction{
		newTx(types.BytesToAddress(nil), 5, 100),
	}, 2)
	nonce, balance = pendingTxs.GetProjection(prevNonce, prevBalance)
	r.Equal(prevNonce+2, nonce)
	r.Equal(prevBalance-50-950, balance)
}
